package p2p

import (
	"context"
	"errors"
	"fmt"
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

type Server struct {
	Info    *metainfo.Info
	PeerID  [20]byte
	Storage *storage.Storage
	Port    int

	mu             sync.Mutex
	knownPeers     map[string]*tracker.Peer
	deniedPeers    map[string]struct{}
	activePeers    map[string]*Peer
	indexToPeers   map[int]map[*Peer]struct{}
	cancelsToPeers map[blockRequest]map[*Peer]struct{}

	tcp    net.Listener
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

func New(info *metainfo.Info, peerID [20]byte, storage *storage.Storage, port int) *Server {
	return &Server{
		Info:    info,
		PeerID:  peerID,
		Storage: storage,
		Port:    port,

		knownPeers:     make(map[string]*tracker.Peer),
		deniedPeers:    make(map[string]struct{}),
		activePeers:    make(map[string]*Peer),
		indexToPeers:   make(map[int]map[*Peer]struct{}),
		cancelsToPeers: make(map[blockRequest]map[*Peer]struct{}),
	}
}

func (p2p *Server) ListenAndServe() error {
	err := p2p.Storage.Load()
	if err != nil {
		return err
	}
	p2p.tcp, err = net.Listen("tcp", fmt.Sprintf(":%d", p2p.Port))
	if err != nil {
		return err
	}
	slog.Info("ListenAndServe", "network", "tcp", "port", p2p.Port)
	errorChan := make(chan error)
	ctx, cancel := context.WithCancel(context.Background())
	p2p.cancel = cancel
	defer cancel()
	go p2p.Serve(ctx, errorChan)
	go p2p.every(ctx, errorChan, p2p.DialPeers, 10*time.Second)
	go p2p.every(ctx, errorChan, p2p.UnchokePeers, 10*time.Second)
	go p2p.every(ctx, errorChan, p2p.RequestPieces, 10*time.Second)
	for {
		select {
		case err := <-errorChan:
			if pe, ok := err.(*PeerError); ok {
				slog.Error("ListenAndServe.Disconnect", "err", err)
				if err := p2p.Disconnect(pe.Peer); err != nil {
					slog.Error("ListenAndServe.Disconnect", "err", err)
				}
			} else {
				return err
			}
		case <-ctx.Done():
			// drain errors
			var errs []error
			for err := range errorChan {
				errs = append(errs, err)
			}
			err = errors.Join(errs...)
			return err
		}
	}
}

func (p2p *Server) Close() error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	if p2p.cancel == nil {
		return errors.New("server not started?")
	}
	p2p.cancel()
	var errs []error
	for _, p := range p2p.activePeers {
		errs = append(errs, p2p.disconnect(p))
	}
	return errors.Join(errs...)
}

func (p2p *Server) Serve(ctx context.Context, errorChan chan error) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn, err := p2p.tcp.Accept()
		if err != nil {
			errorChan <- err
			continue
		}
		go func() {
			peer := p2p.fromConn(conn)
			pctx, cancel := context.WithCancel(ctx)
			peer.cancel = cancel
			if err := p2p.Connect(peer); err != nil {
				errorChan <- &PeerError{peer, err}
				return
			}
			go peer.serve(pctx)
			slog.Info("Serve", "peer", peer.RemoteAddr().String())
		}()
	}
}

func (p2p *Server) DialPeers(ctx context.Context, errorChan chan error) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	var newPeers []*tracker.Peer
	for addr, tp := range p2p.knownPeers {
		if _, ok := p2p.activePeers[addr]; !ok {
			if _, ok := p2p.deniedPeers[addr]; !ok {
				newPeers = append(newPeers, tp)
			}
		}
	}
	var wg sync.WaitGroup
	for _, tp := range newPeers {
		wg.Add(1)
		go func(tp *tracker.Peer) {
			defer wg.Done()
			peer, err := p2p.DialTCP(tp)
			if err != nil {
				slog.Error("DialPeers.dialTCP", "ip", tp.IP, "port", tp.Port, "err", err)
				return
			}
			pctx, cancel := context.WithCancel(ctx)
			peer.cancel = cancel
			if err := p2p.Connect(peer); err != nil {
				errorChan <- &PeerError{peer, err}
				return
			}
			go peer.serve(pctx)
		}(tp)
	}
	wg.Wait()
	return nil
}

func (p2p *Server) UnchokePeers(ctx context.Context, errorChan chan error) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range p2p.activePeers {
		if !p.Choked {
			continue
		}
		wg.Add(1)
		go func(p *Peer) {
			defer wg.Done()
			if err := p.Unchoke(); err != nil {
				errorChan <- err
			}
		}(p)
	}
	wg.Wait()
	return nil
}

func (p2p *Server) RequestPieces(ctx context.Context, errorChan chan error) error {
	bf := p2p.Storage.Torrent.Bitfield()
outer:
	for i := range len(p2p.Info.Pieces) {
		if bf.Has(i) {
			continue
		}
		p2p.mu.Lock()
		peers := maps.Clone(p2p.indexToPeers[i])
		p2p.mu.Unlock()
		for p := range peers {
			if p.RemoteChoked {
				continue
			}
			err := p.requestPiece(ctx, i)
			if err != nil {
				errorChan <- &PeerError{p, err}
			}
			p2p.mu.Lock()
			defer p2p.mu.Unlock()
			for _, p := range p2p.activePeers {
				err := p.Send(peerapi.Have(i))
				if err != nil {
					return err
				}
			}
			continue outer
		}
	}
	return nil
}

func addr(tp *tracker.Peer) (string, error) {
	ip := net.ParseIP(tp.IP)
	if ip == nil {
		return "", errors.New("bad ip")
	}
	return fmt.Sprintf("%s:%d", ip.String(), tp.Port), nil
}

func (p2p *Server) AddPeer(tp *tracker.Peer) error {
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

func (p2p *Server) RemovePeer(tp *tracker.Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	addr, err := addr(tp)
	if err != nil {
		return err
	}
	delete(p2p.knownPeers, addr)
	return nil
}

func (p2p *Server) Connect(peer *Peer) error {
	slog.Debug("Connect", "peer", peer.RemoteAddr().String())
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	if _, ok := p2p.activePeers[peer.RemoteAddr().String()]; ok {
		return errors.New("p2p: already connected")
	}
	hs := &peerapi.Handshake{
		InfoHash: p2p.Info.SHA1,
		PeerID:   p2p.PeerID,
	}
	if err := peer.Handshake(hs); err != nil {
		return &PeerError{peer, err}
	}
	bf := p2p.Storage.Torrent.Bitfield()
	if err := peer.Send(peerapi.Bitfield(bf)); err != nil {
		return &PeerError{peer, err}
	}
	p2p.activePeers[peer.RemoteAddr().String()] = peer
	return nil
}

func (p2p *Server) Disconnect(peer *Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	if _, ok := p2p.activePeers[peer.RemoteAddr().String()]; !ok {
		return nil
	}
	slog.Debug("Disconnect", "peer", peer.ID)
	return p2p.disconnect(peer)
}

func (p2p *Server) disconnect(peer *Peer) error {
	delete(p2p.activePeers, peer.RemoteAddr().String())
	for _, i := range peer.Bitfield.Items() {
		delete(p2p.indexToPeers[i], peer)
	}
	if err := peer.Close(); err != nil {
		return &PeerError{peer, err}
	}
	return nil
}

func (p2p *Server) Deny(peer *Peer) error {
	if err := p2p.Disconnect(peer); err != nil {
		return err
	}
	p2p.mu.Lock()
	p2p.deniedPeers[peer.RemoteAddr().String()] = struct{}{}
	p2p.mu.Unlock()
	return nil
}

func (p2p *Server) DialTCP(peer *tracker.Peer) (*Peer, error) {
	slog.Debug("DialTCP", "ip", peer.IP, "port", peer.Port)
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", peer.IP, peer.Port))
	if err != nil {
		return nil, fmt.Errorf("DialTCP: %w", err)
	}
	p := p2p.fromConn(conn)
	p.ID = peer.ID
	return p, nil
}

func (p2p *Server) fromConn(conn net.Conn) *Peer {
	return &Peer{
		server:           p2p,
		Conn:             conn,
		RemoteChoked:     true,
		Choked:           true,
		inflightRequests: map[blockRequest]chan error{},
		canceledRequests: map[blockRequest]struct{}{},
	}
}

func (p2p *Server) every(ctx context.Context, errorChan chan error, f func(ctx context.Context, errChan chan error) error, d time.Duration) {
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := f(ctx, errorChan); err != nil {
			errorChan <- err
			return
		}
	}
}
