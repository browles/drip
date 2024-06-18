package p2p

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"net"
	"sync"
	"time"

	"github.com/browles/drip/api/metainfo"
	peerapi "github.com/browles/drip/api/peer"
	"github.com/browles/drip/api/tracker"
	"github.com/browles/drip/storage"
)

type Peer2PeerServer struct {
	Info    *metainfo.Info
	PeerID  [20]byte
	Storage *storage.Storage
	Port    int

	mu               sync.Mutex
	knownPeers       map[string]*tracker.Peer
	deniedPeers      map[string]struct{}
	activePeers      map[string]*peerWithCancel
	indexToPeers     map[int]map[string]struct{}
	cancelsToPeerIDs map[blockRequest]map[string]struct{}

	errors chan error
	tcp    net.Listener
}

type peerWithCancel struct {
	*Peer
	cancel context.CancelFunc
}

type blockRequest struct {
	index  int
	begin  int
	length int
}

type PeerError struct {
	Peer *Peer
	err  error
}

func (pe *PeerError) Error() string {
	return fmt.Sprintf("peer id=%s: %s", pe.Peer.ID, pe.err)
}

func (pe *PeerError) Unwrap() error {
	return pe.err
}

func New(info *metainfo.Info, peerID [20]byte, storage *storage.Storage, port int) *Peer2PeerServer {
	return &Peer2PeerServer{
		Info:    info,
		PeerID:  peerID,
		Storage: storage,
		Port:    port,

		knownPeers:       make(map[string]*tracker.Peer),
		deniedPeers:      make(map[string]struct{}),
		activePeers:      make(map[string]*peerWithCancel),
		indexToPeers:     make(map[int]map[string]struct{}),
		cancelsToPeerIDs: make(map[blockRequest]map[string]struct{}),

		errors: make(chan error),
	}
}

func (p2p *Peer2PeerServer) Start(ctx context.Context) error {
	err := p2p.Storage.AddTorrent(p2p.Info)
	if err != nil {
		return err
	}
	var lc net.ListenConfig
	p2p.tcp, err = lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", p2p.Port))
	if err != nil {
		return err
	}
	log.Printf("Listening on port: %d", p2p.Port)
	go p2p.every(ctx, func() error { return p2p.DialPeers(ctx) }, 10*time.Second)
	go p2p.every(ctx, func() error { return p2p.UnchokePeers() }, 10*time.Second)
	go p2p.every(ctx, func() error { return p2p.RequestPieces(ctx) }, 10*time.Second)
	go p2p.Listen(ctx)
	go p2p.HandleErrors(ctx)
	return nil
}

func (p2p *Peer2PeerServer) DialPeers(ctx context.Context) error {
	p2p.mu.Lock()
	active := len(p2p.activePeers)
	p2p.mu.Unlock()
	if active >= 50 {
		return nil
	}
	p2p.mu.Lock()
	var newPeers []*tracker.Peer
	for _, tp := range p2p.knownPeers {
		if _, ok := p2p.activePeers[tp.Addr()]; !ok {
			newPeers = append(newPeers, tp)
		}
	}
	p2p.mu.Unlock()
	for _, tp := range newPeers {
		peer, err := DialTCP(tp)
		if err != nil {
			return err
		}
		if err = p2p.Connect(ctx, peer); err != nil {
			return err
		}
	}
	return nil
}

// TODO refactor: combine Listen, Connect, and HandlePeer
func (p2p *Peer2PeerServer) Listen(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn, err := p2p.tcp.Accept()
		if err != nil {
			p2p.errors <- err
		}
		peer := FromConn(conn)
		if err := p2p.Connect(ctx, peer); err != nil {
			p2p.errors <- &PeerError{peer, err}
		}
	}
}

func (p2p *Peer2PeerServer) UnchokePeers() error {
	var choked []*peerWithCancel
	p2p.mu.Lock()
	for _, p := range p2p.activePeers {
		if p.Choked {
			choked = append(choked, p)
		}
	}
	p2p.mu.Unlock()
	for _, p := range choked {
		if err := p.Unchoke(); err != nil {
			return err
		}
	}
	return nil
}

func (p2p *Peer2PeerServer) RequestPieces(ctx context.Context) error {
	torrent := p2p.Storage.GetTorrent(p2p.Info.SHA1)
	bf := torrent.Bitfield()
outer:
	for i := range len(p2p.Info.Pieces) {
		if bf.Has(i) {
			continue
		}
		p2p.mu.Lock()
		addrs := maps.Clone(p2p.indexToPeers[i])
		p2p.mu.Unlock()
		for addr := range addrs {
			p2p.mu.Lock()
			p, ok := p2p.activePeers[addr]
			if !ok {
				log.Print("Addr disappeared: " + addr)
				delete(p2p.indexToPeers[i], addr)
				p2p.mu.Unlock()
				continue
			}
			p2p.mu.Unlock()
			if p.RemoteChoked {
				continue
			}
			err := p2p.requestPiece(ctx, p.Peer, i)
			if err != nil {
				return &PeerError{p.Peer, err}
			}
			continue outer
		}
	}
	return nil
}

func (p2p *Peer2PeerServer) HandleErrors(ctx context.Context) {
	for {
		var err error
		select {
		case <-ctx.Done():
			return
		case err = <-p2p.errors:
			if pe, ok := err.(*PeerError); ok {
				slog.Error("HandleErrors", "err", pe.Error())
				if err := p2p.Disconnect(pe.Peer); err != nil {
					slog.Error("Disconnect", "err", pe.Error())
				}
				continue
			} else {
				log.Fatal(err)
			}
		}
	}
}

func (p2p *Peer2PeerServer) AddPeer(p *tracker.Peer) {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	if _, ok := p2p.knownPeers[p.Addr()]; ok {
		return
	}
	p2p.knownPeers[p.Addr()] = p
}

func (p2p *Peer2PeerServer) RemovePeer(p *tracker.Peer) {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	delete(p2p.knownPeers, p.Addr())
}

func (p2p *Peer2PeerServer) Connect(ctx context.Context, peer *Peer) error {
	hs := &peerapi.Handshake{
		InfoHash: p2p.Info.SHA1,
		PeerID:   p2p.PeerID,
	}
	if err := peer.Handshake(hs); err != nil {
		return &PeerError{peer, err}
	}
	slog.Debug("Connect", "peer", peer.ID)
	torrent := p2p.Storage.GetTorrent(p2p.Info.SHA1)
	if err := peer.Send(peerapi.Bitfield(torrent.Bitfield())); err != nil {
		return &PeerError{peer, err}
	}
	ctx, cancel := context.WithCancel(ctx)
	p2p.mu.Lock()
	p2p.activePeers[peer.RemoteAddr().String()] = &peerWithCancel{peer, cancel}
	p2p.mu.Unlock()
	go p2p.HandlePeer(ctx, peer)
	go p2p.every(ctx, func() error {
		return p2p.keepalive(peer)
	}, 2*time.Minute)
	return nil
}

func (p2p *Peer2PeerServer) keepalive(peer *Peer) error {
	if err := peer.Keepalive(); err != nil {
		return &PeerError{peer, err}
	}
	return nil
}

func (p2p *Peer2PeerServer) Disconnect(peer *Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	if pwc, ok := p2p.activePeers[peer.RemoteAddr().String()]; ok {
		slog.Debug("Disconnect", "peer", peer.ID)
		pwc.cancel()
	} else {
		return nil
	}
	delete(p2p.activePeers, peer.ID)
	for _, i := range peer.Bitfield.Items() {
		delete(p2p.indexToPeers[i], peer.RemoteAddr().String())
	}
	peer.Close()
	return nil
}

func (p2p *Peer2PeerServer) Deny(peer *Peer) error {
	if err := p2p.Disconnect(peer); err != nil {
		return err
	}
	p2p.mu.Lock()
	p2p.deniedPeers[peer.ID] = struct{}{}
	p2p.mu.Unlock()
	return nil
}

func (p2p *Peer2PeerServer) HandlePeer(ctx context.Context, peer *Peer) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		m, err := peer.Receive()
		if err != nil {
			p2p.errors <- &PeerError{peer, err}
			return
		}
		err = p2p.HandleMessage(peer, m)
		if err != nil {
			p2p.errors <- &PeerError{peer, err}
			return
		}
	}
}

func (p2p *Peer2PeerServer) HandleMessage(peer *Peer, m *peerapi.Message) error {
	switch m.Type {
	case peerapi.KEEPALIVE:
	case peerapi.CHOKE:
		peer.RemoteChoked = true
	case peerapi.UNCHOKE:
		peer.RemoteChoked = false
	case peerapi.INTERESTED:
		peer.RemoteInterested = true
	case peerapi.NOT_INTERESTED:
		peer.RemoteInterested = false
	case peerapi.HAVE:
		p2p.HandleHave(peer, m.Index())
	case peerapi.BITFIELD:
		bf := m.Bitfield()
		for _, i := range bf.Items() {
			p2p.HandleHave(peer, i)
		}
	case peerapi.REQUEST:
		p2p.HandleRequest(peer, m.Index(), m.Begin(), m.Length())
	case peerapi.CANCEL:
		p2p.HandleCancel(peer, m.Index(), m.Begin(), m.Length())
	case peerapi.PIECE:
		p2p.HandlePiece(peer, m.Index(), m.Begin(), m.Piece())
	}
	return nil
}

func (p2p *Peer2PeerServer) HandleHave(peer *Peer, i int) {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	peer.Bitfield.Add(i)
	m, ok := p2p.indexToPeers[i]
	if !ok {
		m = make(map[string]struct{})
		p2p.indexToPeers[i] = m
	}
	m[peer.RemoteAddr().String()] = struct{}{}
}

func (p2p *Peer2PeerServer) HandleRequest(peer *Peer, index, begin, length int) {
	p2p.sendBlock(peer, index, begin, length)
}

func (p2p *Peer2PeerServer) HandleCancel(peer *Peer, index, begin, length int) {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	k := blockRequest{index, begin, length}
	m, ok := p2p.cancelsToPeerIDs[k]
	if !ok {
		m = make(map[string]struct{})
		p2p.cancelsToPeerIDs[k] = m
	}
	m[peer.ID] = struct{}{}
}

func (p2p *Peer2PeerServer) HandlePiece(peer *Peer, index, begin int, piece []byte) {
	p2p.storePiece(peer, index, begin, piece)
}

// Elastic queue?:
// n = 4
// expected = 64kps
// fire off n requests, start = now()
// after last completes, delta = time.since(start)
// rate = n*blocksize / delta
// n *= clamp(rate / expected, 0.5, 2.0)
// expected = rate

func (p2p *Peer2PeerServer) requestPiece(ctx context.Context, peer *Peer, index int) (err error) {
	start := time.Now()
	slog.Debug("requestPiece", "peer", peer.ID, "index", index)
	defer func() {
		slog.Debug("requestPiece.time", "peer", peer.ID, "index", index, "time", time.Since(start), "err", err)
	}()
	piece := p2p.Storage.GetPiece(p2p.Info.SHA1, index)
	piece.Reset()
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 16) // todo: adaptive queue
	errs := make(chan error)
	pieceLength := p2p.Info.GetPieceLength(index)
	for begin := 0; begin < pieceLength; begin += storage.BLOCK_LENGTH {
		blockLength := min(storage.BLOCK_LENGTH, pieceLength-begin)
		wg.Add(1)
		semaphore <- struct{}{}
		go func(begin, blockLength int) {
			defer wg.Done()
			defer func() { <-semaphore }()
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			err := peer.RequestBlock(ctx, &blockRequest{index, begin, blockLength})
			if err != nil {
				errs <- err
			}
		}(begin, blockLength)
	}
	go func() {
		wg.Wait()
		close(errs)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errs:
		// Drain errs
		for range errs {
		}
		return err
	case <-piece.Done:
		if err := piece.Err(); err != nil {
			return err
		}
	}
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	for _, p := range p2p.activePeers {
		err := p.Send(peerapi.Have(index))
		if err != nil {
			return err
		}
	}
	return nil
}

func (p2p *Peer2PeerServer) sendBlock(peer *Peer, index, begin, length int) {
	if peer.Choked {
		return // Peer is not allowed requests
	}
	req := blockRequest{index, begin, length}
	p2p.mu.Lock()
	if _, ok := p2p.cancelsToPeerIDs[req][peer.ID]; ok {
		delete(p2p.cancelsToPeerIDs[req], peer.ID)
		p2p.mu.Unlock()
		return
	}
	p2p.mu.Unlock()
	data, err := p2p.Storage.GetBlock(p2p.Info.SHA1, req.index, req.begin, req.length)
	if err != nil {
		p2p.errors <- &PeerError{peer, err}
		return
	}
	err = peer.Send(peerapi.Piece(req.index, req.begin, data))
	if err != nil {
		p2p.errors <- &PeerError{peer, err}
		return
	}
	// todo: record stats
}

func (p2p *Peer2PeerServer) storePiece(peer *Peer, index, begin int, data []byte) {
	err := p2p.Storage.PutBlock(p2p.Info.SHA1, index, begin, data)
	if err != nil {
		if !errors.Is(err, storage.ErrBlockExists) {
			p2p.errors <- &PeerError{peer, err}
		}
		return
	}
	// todo: record stats
}

func (p2p *Peer2PeerServer) every(ctx context.Context, f func() error, d time.Duration) {
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	if err := f(); err != nil {
		p2p.errors <- err
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := f(); err != nil {
			p2p.errors <- err
		}
	}
}
