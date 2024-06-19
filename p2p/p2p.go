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
	activePeers      map[string]*Peer
	indexToPeers     map[int]map[string]struct{}
	cancelsToPeerIDs map[blockRequest]map[string]struct{}

	tcp net.Listener
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
		activePeers:      make(map[string]*Peer),
		indexToPeers:     make(map[int]map[string]struct{}),
		cancelsToPeerIDs: make(map[blockRequest]map[string]struct{}),
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
	slog.Info("Start.Listen", "network", "tcp", "port", p2p.Port)
	errors := make(chan error)
	go p2p.every(ctx, errors, func() error { return p2p.DialPeers(ctx) }, 10*time.Second)
	go p2p.every(ctx, errors, func() error { return p2p.UnchokePeers() }, 10*time.Second)
	go p2p.every(ctx, errors, func() error { return p2p.RequestPieces(ctx) }, 10*time.Second)
	go p2p.Listen(ctx, errors)
	go p2p.HandleErrors(ctx, errors)
	return nil
}

func (p2p *Peer2PeerServer) DialPeers(ctx context.Context) error {
	p2p.mu.Lock()
	if len(p2p.activePeers) >= 50 {
		p2p.mu.Unlock()
		return nil
	}
	var newPeers []*tracker.Peer
	for addr, tp := range p2p.knownPeers {
		if _, ok := p2p.activePeers[addr]; !ok {
			if _, ok := p2p.deniedPeers[addr]; !ok {
				newPeers = append(newPeers, tp)
			}
		}
	}
	p2p.mu.Unlock()
	for _, tp := range newPeers {
		peer, err := DialTCP(tp)
		if err != nil {
			slog.Error("DialPeers", "ip", tp.IP, "port", tp.Port, "err", err)
			continue
		}
		go p2p.HandlePeer(ctx, peer)
	}
	return nil
}

func (p2p *Peer2PeerServer) Listen(ctx context.Context, errors chan error) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn, err := p2p.tcp.Accept()
		if err != nil {
			errors <- err
			return
		}
		go p2p.HandlePeer(ctx, FromConn(conn))
	}
}

func (p2p *Peer2PeerServer) UnchokePeers() error {
	var choked []*Peer
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
				delete(p2p.indexToPeers[i], addr)
				p2p.mu.Unlock()
				continue
			}
			p2p.mu.Unlock()
			if p.RemoteChoked {
				continue
			}
			err := p2p.requestPiece(ctx, p, i)
			if err != nil {
				return &PeerError{p, err}
			}
			continue outer
		}
	}
	return nil
}

func (p2p *Peer2PeerServer) HandleErrors(ctx context.Context, errors chan error) {
	for {
		var err error
		select {
		case <-ctx.Done():
			return
		case err = <-errors:
			slog.Error("HandleErrors", "err", err)
			if pe, ok := err.(*PeerError); ok {
				if err := p2p.Disconnect(pe.Peer); err != nil {
					slog.Error("HandleErrors.Disconnect", "err", pe.Error())
				}
				continue
			} else {
				log.Fatal(err)
			}
		}
	}
}

func addr(tp *tracker.Peer) (string, error) {
	ip := net.ParseIP(tp.IP)
	if ip == nil {
		return "", errors.New("bad ip")
	}
	return fmt.Sprintf("%s:%d", ip.String(), tp.Port), nil
}

func (p2p *Peer2PeerServer) AddPeer(tp *tracker.Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	addr, err := addr(tp)
	if err != nil {
		return err
	}
	if _, ok := p2p.knownPeers[addr]; ok {
		return nil
	}
	p2p.knownPeers[addr] = tp
	return nil
}

func (p2p *Peer2PeerServer) RemovePeer(tp *tracker.Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	addr, err := addr(tp)
	if err != nil {
		return err
	}
	delete(p2p.knownPeers, addr)
	return nil
}

func (p2p *Peer2PeerServer) Connect(peer *Peer) error {
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
	p2p.mu.Lock()
	p2p.activePeers[peer.RemoteAddr().String()] = peer
	p2p.mu.Unlock()
	return nil
}

func (p2p *Peer2PeerServer) Disconnect(peer *Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	if _, ok := p2p.activePeers[peer.RemoteAddr().String()]; ok {
		slog.Debug("Disconnect", "peer", peer.ID)
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
	p2p.deniedPeers[peer.RemoteAddr().String()] = struct{}{}
	p2p.mu.Unlock()
	return nil
}

func (p2p *Peer2PeerServer) HandlePeer(ctx context.Context, peer *Peer) {
	err := p2p.Connect(peer)
	if err != nil {
		slog.Error("HandlePeer", "err", err)
		return
	}
	errors := make(chan error)
	go p2p.every(ctx, errors, peer.Keepalive, 2*time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-errors:
			if err != nil {
				slog.Error("HandlePeer", "err", err)
				if err := p2p.Disconnect(peer); err != nil {
					slog.Error("HandlePeer.Disconnect", "err", err)
				}
				return
			}
		default:
		}
		m, err := peer.Receive()
		if err != nil {
			slog.Error("HandlePeer", "err", err)
			if err := p2p.Disconnect(peer); err != nil {
				slog.Error("HandlePeer.Disconnect", "err", err)
			}
			return
		}
		p2p.HandleMessage(ctx, errors, peer, m)
	}
}

func (p2p *Peer2PeerServer) HandleMessage(ctx context.Context, errors chan error, peer *Peer, m *peerapi.Message) {
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
		p2p.HandleRequest(ctx, errors, peer, m.Index(), m.Begin(), m.Length())
	case peerapi.CANCEL:
		p2p.HandleCancel(peer, m.Index(), m.Begin(), m.Length())
	case peerapi.PIECE:
		p2p.HandlePiece(ctx, errors, peer, m.Index(), m.Begin(), m.Piece())
	}
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

func (p2p *Peer2PeerServer) HandleRequest(ctx context.Context, errors chan error, peer *Peer, index, begin, length int) {
	go func() {
		if err := p2p.sendBlock(peer, index, begin, length); err != nil {
			errors <- err
		}
	}()
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

func (p2p *Peer2PeerServer) HandlePiece(ctx context.Context, errors chan error, peer *Peer, index, begin int, piece []byte) {
	go func() {
		if err := p2p.storePiece(peer, index, begin, piece); err != nil {
			errors <- err
		}
	}()
}

func (p2p *Peer2PeerServer) requestPiece(ctx context.Context, peer *Peer, index int) (err error) {
	start := time.Now()
	slog.Debug("requestPiece", "peer", peer.ID, "index", index)
	defer func() {
		if err == nil {
			slog.Debug("requestPiece.time", "peer", peer.ID, "index", index, "time", time.Since(start))
		}
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
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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

func (p2p *Peer2PeerServer) sendBlock(peer *Peer, index, begin, length int) error {
	if peer.Choked {
		return ErrChoked
	}
	req := blockRequest{index, begin, length}
	p2p.mu.Lock()
	if _, ok := p2p.cancelsToPeerIDs[req][peer.ID]; ok {
		delete(p2p.cancelsToPeerIDs[req], peer.ID)
		p2p.mu.Unlock()
		return nil
	}
	p2p.mu.Unlock()
	data, err := p2p.Storage.GetBlock(p2p.Info.SHA1, req.index, req.begin, req.length)
	if err != nil {
		return err
	}
	err = peer.Send(peerapi.Piece(req.index, req.begin, data))
	if err != nil {
		return err
	}
	return nil
	// todo: record stats
}

func (p2p *Peer2PeerServer) storePiece(peer *Peer, index, begin int, data []byte) error {
	err := p2p.Storage.PutBlock(p2p.Info.SHA1, index, begin, data)
	if err != nil && !errors.Is(err, storage.ErrBlockExists) {
		return err
	}
	return nil
	// todo: record stats
}

func (p2p *Peer2PeerServer) every(ctx context.Context, errors chan error, f func() error, d time.Duration) {
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	if err := f(); err != nil {
		errors <- err
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := f(); err != nil {
			errors <- err
		}
	}
}
