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
	"github.com/browles/drip/future"
	"github.com/browles/drip/storage"
	"golang.org/x/sync/errgroup"
)

type Server struct {
	Info    *metainfo.Info
	PeerID  [20]byte
	Storage *storage.Storage
	Port    int

	mu             sync.Mutex
	knownPeers     map[string]*tracker.Peer
	blockedPeers   map[string]struct{}
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

func New(info *metainfo.Info, peerID [20]byte, storage *storage.Storage, port int) *Server {
	return &Server{
		Info:    info,
		PeerID:  peerID,
		Storage: storage,
		Port:    port,

		knownPeers:     make(map[string]*tracker.Peer),
		blockedPeers:   make(map[string]struct{}),
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
	ctx, cancel := context.WithCancel(context.Background())
	p2p.cancel = cancel
	defer cancel()
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return p2p.Serve(ctx) })
	eg.Go(func() error { return p2p.every(ctx, p2p.DialPeers, 10*time.Second) })
	eg.Go(func() error { return p2p.every(ctx, p2p.UnchokePeers, 10*time.Second) })
	eg.Go(func() error { return p2p.every(ctx, p2p.RequestPieces, 10*time.Second) })
	return eg.Wait()
}

func (p2p *Server) Close() error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	if p2p.cancel == nil {
		return errors.New("p2p: server not started?")
	}
	p2p.cancel()
	var errs []error
	for _, p := range p2p.activePeers {
		errs = append(errs, p2p.disconnect(p))
	}
	return errors.Join(errs...)
}

func (p2p *Server) Serve(ctx context.Context) error {
	defer p2p.Close()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := p2p.tcp.Accept()
		if err != nil {
			return err
		}
		slog.Debug("Serve.Accept", "conn", conn.RemoteAddr())
		go func() {
			peer := p2p.fromConn(conn)
			ctx, cancel := context.WithCancel(ctx)
			peer.cancel = cancel
			defer peer.Close()
			if err := p2p.Connect(peer); err != nil {
				slog.Error("Serve.Connect", "err", err)
				return
			}
			slog.Info("Serve", "peer", peer.RemoteAddr().String())
			peer.serve(ctx)
		}()
	}
}

func (p2p *Server) DialPeers(ctx context.Context) error {
	p2p.mu.Lock()
	var newPeers []*tracker.Peer
	for addr, tp := range p2p.knownPeers {
		if _, ok := p2p.activePeers[addr]; !ok {
			if _, ok := p2p.blockedPeers[addr]; !ok {
				newPeers = append(newPeers, tp)
			}
		}
	}
	p2p.mu.Unlock()
	var wg sync.WaitGroup
	for _, tp := range newPeers {
		wg.Add(1)
		go func(tp *tracker.Peer) {
			defer wg.Done()
			peer, err := p2p.dialTCP(tp)
			if err != nil {
				slog.Error("DialPeers.DialTCP", "ip", tp.IP, "port", tp.Port, "err", err)
				return
			}
			ctx, cancel := context.WithCancel(ctx)
			peer.cancel = cancel
			if err := p2p.Connect(peer); err != nil {
				slog.Error("DialPeers.Connect", "err", err)
				return
			}
			go peer.serve(ctx)
		}(tp)
	}
	wg.Wait()
	return nil
}

func (p2p *Server) UnchokePeers(ctx context.Context) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range p2p.activePeers {
		if p.Closed || !p.Choked {
			continue
		}
		wg.Add(1)
		go func(p *Peer) {
			defer wg.Done()
			if err := p.Unchoke(); err != nil {
				slog.Error("UnchokePeers.Unchoke", "err", err)
				return
			}
		}(p)
	}
	wg.Wait()
	return nil
}

func (p2p *Server) RequestPieces(ctx context.Context) error {
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
			if p.Closed || p.RemoteChoked {
				continue
			}
			if err := p.requestPiece(ctx, i); err != nil {
				slog.Error("RequestPieces.requestPiece", "err", err)
				if _, ok := err.(*storage.ChecksumError); ok {
					if err := p2p.Block(p); err != nil {
						slog.Error("RequestPieces.Block", "err", err)
					}
				}
				continue
			}
			p2p.mu.Lock()
			peers := maps.Clone(p2p.activePeers)
			p2p.mu.Unlock()
			for _, p := range peers {
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
	p2p.mu.Lock()
	if _, ok := p2p.activePeers[peer.RemoteAddr().String()]; ok {
		return errors.New("p2p: already connected")
	}
	p2p.mu.Unlock()
	slog.Debug("Connect", "peer", peer.RemoteAddr().String())
	hs := &peerapi.Handshake{
		InfoHash: p2p.Info.SHA1,
		PeerID:   p2p.PeerID,
	}
	if err := peer.Handshake(hs); err != nil {
		return err
	}
	bf := p2p.Storage.Torrent.Bitfield()
	if err := peer.Send(peerapi.Bitfield(bf)); err != nil {
		return err
	}
	p2p.mu.Lock()
	p2p.activePeers[peer.RemoteAddr().String()] = peer
	p2p.mu.Unlock()
	return nil
}

func (p2p *Server) Disconnect(peer *Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	slog.Debug("Disconnect", "peer", peer.RemoteAddr().String())
	return p2p.disconnect(peer)
}

func (p2p *Server) disconnect(peer *Peer) error {
	delete(p2p.activePeers, peer.RemoteAddr().String())
	for _, i := range peer.Bitfield.Items() {
		delete(p2p.indexToPeers[i], peer)
	}
	return peer.Close()
}

func (p2p *Server) Block(peer *Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	slog.Debug("Block", "peer", peer.RemoteAddr().String())
	p2p.blockedPeers[peer.RemoteAddr().String()] = struct{}{}
	return p2p.disconnect(peer)
}

func (p2p *Server) dialTCP(peer *tracker.Peer) (*Peer, error) {
	slog.Debug("dialTCP", "ip", peer.IP, "port", peer.Port)
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", peer.IP, peer.Port))
	if err != nil {
		return nil, err
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
		inflightRequests: make(map[blockRequest]*future.Future[error]),
		canceledRequests: make(map[blockRequest]struct{}),
	}
}

func (p2p *Server) every(ctx context.Context, f func(ctx context.Context) error, d time.Duration) error {
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if err := f(ctx); err != nil {
			return err
		}
	}
}
