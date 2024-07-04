package p2p

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	peerapi "github.com/browles/drip/api/peer"
	"github.com/browles/drip/bitfield"
	"github.com/browles/drip/future"
	"github.com/browles/drip/storage"
	"golang.org/x/sync/errgroup"
)

type Peer struct {
	ID               [20]byte
	RemoteChoked     atomic.Bool
	RemoteInterested atomic.Bool
	Closed           atomic.Bool
	Choked           atomic.Bool
	Interested       atomic.Bool
	Bitfield         bitfield.Bitfield

	server     *Server
	cancel     context.CancelFunc
	downloaded atomic.Int64
	uploaded   atomic.Int64

	mu sync.Mutex
	net.Conn
	inflightRequests map[blockRequest]*future.Future[error]
	canceledRequests map[blockRequest]struct{}
}

type blockRequest struct {
	index  int
	begin  int
	length int
}

var ErrChoked = errors.New("p2p: peer connection is choked")

func (p *Peer) Close() error {
	if p.Closed.CompareAndSwap(false, true) {
		if p.cancel == nil {
			panic("peer missing cancel")
		}
		p.cancel()
		return p.Conn.Close()
	}
	return nil
}

func (p *Peer) Handshake() error {
	slog.Debug("Handshake", "peer", p.RemoteAddr())
	p.SetDeadline(time.Now().Add(10 * time.Second))
	defer p.SetDeadline(time.Time{})
	hs := &peerapi.Handshake{
		InfoHash: p.server.Info.SHA1,
		PeerID:   p.server.PeerID,
	}
	if err := peerapi.Write(p.Conn, hs); err != nil {
		return err
	}
	phs, err := peerapi.ReadHandshake(p.Conn)
	if err != nil {
		return err
	}
	if phs.InfoHash != hs.InfoHash {
		return errors.New("p2p: Handshake: info hashes do not match")
	}
	if p.ID != [20]byte{} && p.ID != phs.PeerID {
		return errors.New("p2p: Handshake: peer IDs do not match")
	}
	p.ID = phs.PeerID
	if p.ID == p.server.PeerID {
		return errors.New("p2p: Handshake: peer ID matches own ID")
	}
	return nil
}

func (p *Peer) IPID() IPID {
	addr := p.RemoteAddr().(*net.TCPAddr)
	return IPID(fmt.Sprintf("%s[%s]", string(p.ID[:]), addr.IP.String()))
}

func (p *Peer) Send(m *peerapi.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if m.Type == peerapi.REQUEST {
		if p.RemoteChoked.Load() {
			return ErrChoked
		}
		p.SetReadDeadline(time.Now().Add(2 * time.Minute))
	}
	slog.Debug("Send", "peer", p.ID, "message", m)
	return peerapi.Write(p.Conn, m)
}

func (p *Peer) Receive() (m *peerapi.Message, err error) {
	p.SetReadDeadline(time.Now().Add(2 * time.Minute))
	m, err = peerapi.ReadMessage(p.Conn)
	if err != nil {
		return nil, err
	}
	slog.Debug("Receive", "peer", p.ID, "message", m)
	switch m.Type {
	case peerapi.PIECE:
		p.mu.Lock()
		br := blockRequest{m.Index(), m.Begin(), len(m.Piece())}
		p.completeRequest(&br, nil)
		p.mu.Unlock()
	case peerapi.CHOKE:
		p.mu.Lock()
		for br := range p.inflightRequests {
			p.completeRequest(&br, ErrChoked)
		}
		p.mu.Unlock()
	default:
	}
	return m, nil
}

func (p *Peer) Keepalive() error {
	return p.Send(peerapi.Keepalive())
}

func (p *Peer) Choke() error {
	if p.Choked.CompareAndSwap(false, true) {
		return p.Send(peerapi.Choke())
	}
	return nil
}

func (p *Peer) Unchoke() error {
	if p.Choked.CompareAndSwap(true, false) {
		return p.Send(peerapi.Unchoke())
	}
	return nil
}

func (p *Peer) Interest() error {
	if p.Interested.CompareAndSwap(false, true) {
		return p.Send(peerapi.Interested())
	}
	return nil
}

func (p *Peer) NotInterest() error {
	if p.Interested.CompareAndSwap(true, false) {
		return p.Send(peerapi.NotInterested())
	}
	return nil
}

func (peer *Peer) serve(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		m, err := peer.Receive()
		if err != nil {
			slog.Error("serve.Receive", "err", err)
			peer.server.Disconnect(peer)
			return
		}
		if err = peer.HandleMessage(m); err != nil {
			slog.Error("serve.HandleMessage", "err", err)
			peer.server.Disconnect(peer)
			return
		}
	}
}

func (peer *Peer) HandleMessage(m *peerapi.Message) error {
	switch m.Type {
	case peerapi.KEEPALIVE:
	case peerapi.CHOKE:
		peer.RemoteChoked.Store(true)
	case peerapi.UNCHOKE:
		peer.RemoteChoked.Store(false)
		peer.server.QueuePeer(peer)
	case peerapi.INTERESTED:
		peer.RemoteInterested.Store(true)
	case peerapi.NOT_INTERESTED:
		peer.RemoteInterested.Store(false)
	case peerapi.HAVE:
		return peer.HandleHave(m.Index())
	case peerapi.BITFIELD:
		bf := m.Bitfield()
		for _, i := range bf.Items() {
			if err := peer.HandleHave(i); err != nil {
				return err
			}
		}
	case peerapi.REQUEST:
		return peer.HandleRequest(m.Index(), m.Begin(), m.Length())
	case peerapi.CANCEL:
		peer.HandleCancel(m.Index(), m.Begin(), m.Length())
	case peerapi.PIECE:
		return peer.HandlePiece(m.Index(), m.Begin(), m.Piece())
	}
	return nil
}

func (peer *Peer) HandleHave(i int) error {
	peer.Bitfield.Add(i)
	peer.server.RecordIndex(i, peer)
	if !peer.server.Storage.Torrent.Bitfield.Has(i) {
		peer.server.QueuePeer(peer)
		return peer.Interest()
	}
	return nil
}

func (peer *Peer) HandleRequest(index, begin, length int) error {
	if peer.Choked.Load() {
		return ErrChoked
	}
	req := blockRequest{index, begin, length}
	peer.mu.Lock()
	if _, ok := peer.canceledRequests[req]; ok {
		delete(peer.canceledRequests, req)
		peer.mu.Unlock()
		return nil
	}
	peer.mu.Unlock()
	data, err := peer.server.Storage.GetBlock(req.index, req.begin, req.length)
	if err != nil {
		return err
	}
	err = peer.Send(peerapi.Piece(req.index, req.begin, data))
	if err != nil {
		return err
	}
	peer.uploaded.Add(int64(len(data)))
	peer.server.uploaded.Add(int64(len(data)))
	return nil
}

func (peer *Peer) HandleCancel(index, begin, length int) {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	k := blockRequest{index, begin, length}
	peer.canceledRequests[k] = struct{}{}
}

func (peer *Peer) HandlePiece(index, begin int, data []byte) error {
	err := peer.server.Storage.PutBlock(index, begin, data)
	if err != nil && !errors.Is(err, storage.ErrBlockExists) {
		return err
	}
	if err == nil {
		peer.downloaded.Add(int64(len(data)))
		peer.server.downloaded.Add(int64(len(data)))
	}
	return nil
}

func (peer *Peer) requestPiece(ctx context.Context, index int) (err error) {
	slog.Debug("requestPiece", "peer", peer.ID, "index", index)
	pieceLength := peer.server.Info.GetPieceLength(index)
	piece := peer.server.Storage.ResetPiece(index)
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(16)
	for begin := 0; begin < pieceLength; begin += storage.BLOCK_LENGTH {
		blockLength := min(storage.BLOCK_LENGTH, pieceLength-begin)
		func(begin, blockLength int) {
			eg.Go(func() error {
				ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				return peer.requestBlock(ctx, &blockRequest{index, begin, blockLength})
			})
		}(begin, blockLength)
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	if err := piece.Wait(); err != nil {
		return err
	}
	return nil
}

func (p *Peer) requestBlock(ctx context.Context, br *blockRequest) (err error) {
	if p.RemoteChoked.Load() {
		return ErrChoked
	}
	slog.Debug("requestBlock", "index", br.index, "begin", br.begin, "length", br.length)
	p.mu.Lock()
	fut, ok := p.inflightRequests[*br]
	if !ok {
		fut = future.New[error]()
		p.inflightRequests[*br] = fut
	}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.inflightRequests, *br)
	}()
	if err := p.Send(peerapi.Request(br.index, br.begin, br.length)); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-fut.Done:
		return fut.Wait()
	}
}

func (p *Peer) completeRequest(br *blockRequest, err error) {
	slog.Debug("completeRequest", "index", br.index, "begin", br.begin, "length", br.length, "err", err)
	if fut, ok := p.inflightRequests[*br]; ok {
		fut.Deliver(err)
		delete(p.inflightRequests, *br)
	}
}
