package p2p

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/browles/drip/api/metainfo"
	peerapi "github.com/browles/drip/api/peer"
	"github.com/browles/drip/api/tracker"
	"github.com/browles/drip/bitfield"
	"github.com/browles/drip/future"
	"github.com/browles/drip/storage"
	"golang.org/x/sync/errgroup"
)

type Server struct {
	Info    *metainfo.Info
	PeerID  [20]byte
	Storage *storage.Storage
	Port    int
	Tracker *Multitracker

	peerSemaphore chan struct{}
	newPeerQueue  chan *tracker.Peer
	downloadQueue chan *Peer
	tcp           net.Listener
	cancel        context.CancelFunc
	uploaded      atomic.Int64
	downloaded    atomic.Int64

	mu           sync.Mutex
	knownPeers   map[addr]*tracker.Peer
	knownAddrs   map[idip]map[addr]struct{}
	knownIDIPs   map[addr]idip
	blockedPeers map[addr]struct{}
	activePeers  map[idip]*Peer
	indexToPeers map[int]map[*Peer]struct{}
	queuedPeers  map[*Peer]struct{}
	inflight     map[int]*Peer
}

type (
	addr string // ip:port
	idip string // peer_id[ip]
)

var (
	ErrTooManyConnections = errors.New("p2p: too many connections")
	ErrAlreadyConnected   = errors.New("p2p: already connected")
)

func New(mi *metainfo.Metainfo, peerID [20]byte, storage *storage.Storage, port int) *Server {
	s := &Server{
		Info:    mi.Info,
		PeerID:  peerID,
		Storage: storage,
		Port:    port,

		peerSemaphore: make(chan struct{}, 50),
		newPeerQueue:  make(chan *tracker.Peer, 50),
		downloadQueue: make(chan *Peer),

		knownPeers:   make(map[addr]*tracker.Peer),
		knownAddrs:   make(map[idip]map[addr]struct{}),
		knownIDIPs:   make(map[addr]idip),
		blockedPeers: make(map[addr]struct{}),
		activePeers:  make(map[idip]*Peer),
		indexToPeers: make(map[int]map[*Peer]struct{}),
		queuedPeers:  make(map[*Peer]struct{}),
		inflight:     make(map[int]*Peer),
	}
	s.Tracker = NewMultitracker(mi, s)
	return s
}

func (p2p *Server) Start() error {
	err := p2p.Storage.Load()
	if err != nil {
		return err
	}
	p2p.tcp, err = net.Listen("tcp", fmt.Sprintf(":%d", p2p.Port))
	if err != nil {
		return err
	}
	slog.Info("Start", "network", "tcp", "port", p2p.Port)
	ctx, cancel := context.WithCancel(context.Background())
	p2p.cancel = cancel
	defer cancel()
	eg, ctx := errgroup.WithContext(ctx)
	go func() {
		<-ctx.Done()
		p2p.Stop()
	}()
	eg.Go(func() error { return p2p.Serve(ctx) })
	eg.Go(func() error { return p2p.RequestPieces(ctx) })
	eg.Go(func() error { return p2p.DialPeers(ctx) })
	eg.Go(func() error { return p2p.every(ctx, p2p.UnchokePeers, 10*time.Second) })
	res, err := p2p.Tracker.Start()
	if err != nil {
		return err
	}
	for _, p := range res.Peers.List {
		err := p2p.AddPeer(p)
		if err != nil {
			slog.Error("Start.AddPeer", "err", err)
		}
	}
	eg.Go(func() error { return p2p.every(ctx, p2p.Announce, time.Duration(res.Interval)*time.Second) })
	if err := p2p.UnchokePeers(ctx); err != nil {
		return err
	}
	return eg.Wait()
}

func (p2p *Server) Stop() error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	if p2p.cancel == nil {
		return errors.New("p2p: server not started")
	}
	p2p.cancel()
	p2p.tcp.Close()
	var errs []error
	_, err := p2p.Tracker.Stop()
	errs = append(errs, err)
	for _, p := range p2p.activePeers {
		errs = append(errs, p2p.disconnect(p))
	}
	return errors.Join(errs...)
}

func (p2p *Server) Serve(ctx context.Context) error {
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
		select {
		case p2p.peerSemaphore <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		go func() {
			defer func() { <-p2p.peerSemaphore }()
			slog.Debug("Serve.Accept", "conn", conn.RemoteAddr())
			peer := p2p.newPeer(conn)
			ctx, cancel := context.WithCancel(ctx)
			peer.cancel = cancel
			slog.Info("Serve", "peer", peer.Addr())
			peer.serve(ctx)
		}()
	}
}

func (p2p *Server) DialPeers(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tp := <-p2p.newPeerQueue:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case p2p.peerSemaphore <- struct{}{}:
			}
			go func() {
				defer func() { <-p2p.peerSemaphore }()
				if err := p2p.dialPeer(ctx, tp); err != nil {
					slog.Error("DialPeers.dialPeer", "err", err)
				}
			}()
		}
	}
}

func (p2p *Server) RequestPieces(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case peer := <-p2p.downloadQueue:
			go func() {
				if err := p2p.requestPieceFromPeer(ctx, peer); err != nil {
					slog.Error("RequestPieces.requestPieceFromPeer", "err", err)
				}
			}()
		}
	}
}

func (p2p *Server) UnchokePeers(ctx context.Context) error {
	p2p.mu.Lock()
	peers := maps.Clone(p2p.activePeers)
	p2p.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range peers {
		if p.Closed.Load() || !p.Choked.Load() {
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

func (p2p *Server) Announce(ctx context.Context) error {
	res, err := p2p.Tracker.Announce()
	if err != nil {
		return err
	}
	slog.Debug("Announce", "num_peers", len(res.Peers.List))
	for _, tp := range res.Peers.List {
		err := p2p.AddPeer(tp)
		if err != nil {
			slog.Error("Announce.AddPeer", "err", err)
		}
	}
	return nil
}

func (p2p *Server) QueuePeer(peer *Peer) bool {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	return p2p.queuePeer(peer)
}

func (p2p *Server) AddPeer(tp *tracker.Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	addr, err := remoteAddr(tp)
	if err != nil {
		return err
	}
	if _, ok := p2p.knownPeers[addr]; ok {
		return nil
	}
	if _, ok := p2p.blockedPeers[addr]; ok {
		return nil
	}
	select {
	case p2p.newPeerQueue <- tp:
		p2p.knownPeers[addr] = tp
	default:
	}
	return nil
}

func (p2p *Server) Connect(peer *Peer) error {
	p2p.mu.Lock()
	if len(p2p.activePeers) >= 50 {
		return ErrTooManyConnections
	}
	if idip, ok := p2p.knownIDIPs[peer.Addr()]; ok {
		if _, ok := p2p.activePeers[idip]; ok {
			return ErrAlreadyConnected
		}
	}
	p2p.mu.Unlock()
	slog.Debug("Connect", "peer", peer.Addr())
	if err := peer.Handshake(); err != nil {
		return err
	}
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	if len(p2p.activePeers) >= 50 {
		return ErrTooManyConnections
	}
	addrs, ok := p2p.knownAddrs[peer.IDIP()]
	if !ok {
		addrs = make(map[addr]struct{})
		p2p.knownAddrs[peer.IDIP()] = addrs
	}
	addrs[peer.Addr()] = struct{}{}
	p2p.knownIDIPs[peer.Addr()] = peer.IDIP()
	if _, ok := p2p.activePeers[peer.IDIP()]; ok {
		return ErrAlreadyConnected
	}
	p2p.activePeers[peer.IDIP()] = peer
	bf := p2p.Storage.Torrent.Bitfield
	if err := peer.Send(peerapi.Bitfield(bf)); err != nil {
		return err
	}
	return nil
}

func (p2p *Server) Disconnect(peer *Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	slog.Debug("Disconnect", "peer", peer.Addr())
	return p2p.disconnect(peer)
}

func (p2p *Server) Block(peer *Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	slog.Debug("Block", "peer", peer.Addr())
	for addr := range p2p.knownAddrs[peer.IDIP()] {
		p2p.blockedPeers[addr] = struct{}{}
	}
	return p2p.disconnect(peer)
}

func (p2p *Server) RecordIndex(i int, peer *Peer) {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	m, ok := p2p.indexToPeers[i]
	if !ok {
		m = make(map[*Peer]struct{})
		p2p.indexToPeers[i] = m
	}
	m[peer] = struct{}{}
}

func (p2p *Server) Downloaded() int64 {
	return p2p.downloaded.Load()
}

func (p2p *Server) Uploaded() int64 {
	return p2p.uploaded.Load()
}

func (p2p *Server) dialTCP(peer *tracker.Peer) (*Peer, error) {
	slog.Debug("dialTCP", "ip", peer.IP, "port", peer.Port)
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", peer.IP, peer.Port))
	if err != nil {
		return nil, err
	}
	p := p2p.newPeer(conn)
	copy(p.ID[:], peer.ID)
	return p, nil
}

func (p2p *Server) dialPeer(ctx context.Context, tp *tracker.Peer) error {
	addr, err := remoteAddr(tp)
	if err != nil {
		return err
	}
	p2p.mu.Lock()
	if _, ok := p2p.blockedPeers[addr]; ok {
		p2p.mu.Unlock()
		return nil
	}
	p2p.mu.Unlock()
	peer, err := p2p.dialTCP(tp)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	peer.cancel = cancel
	peer.serve(ctx)
	return nil
}

func (p2p *Server) requestPieceFromPeer(ctx context.Context, peer *Peer) error {
	p2p.mu.Lock()
	defer p2p.mu.Unlock()
	if peer.Closed.Load() || peer.RemoteChoked.Load() {
		delete(p2p.queuedPeers, peer)
		return nil
	}
	bf := p2p.Storage.Torrent.Bitfield
	pieceBf := peer.Bitfield.Difference(bf)
	pieces := pieceBf.Items()
	if len(pieces) == 0 {
		delete(p2p.queuedPeers, peer)
		return peer.NotInterest()
	}
	sort.Slice(pieces, func(i, j int) bool {
		if len(p2p.indexToPeers[i]) == len(p2p.indexToPeers[j]) {
			return rand.Float64() < 0.5
		}
		return len(p2p.indexToPeers[i]) < len(p2p.indexToPeers[j])
	})
	for _, index := range pieces {
		if p2p.inflight[index] != nil {
			continue
		}
		p2p.inflight[index] = peer
		go func() {
			defer delete(p2p.inflight, index)
			if err := p2p.requestPiece(ctx, peer, index); err != nil {
				slog.Error("requestPieceFromPeer.requestPiece", "err", err)
				if _, ok := err.(*storage.ChecksumError); ok {
					if err := p2p.Block(peer); err != nil {
						slog.Error("requestPieceFromPeer.Block", "err", err)
					}
				}
			}
			p2p.downloadQueue <- peer
		}()
		return nil
	}
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			p2p.downloadQueue <- peer
		}
	}()
	return nil
}

func (p2p *Server) requestPiece(ctx context.Context, p *Peer, i int) error {
	if err := p.requestPiece(ctx, i); err != nil {
		return err
	}
	p2p.mu.Lock()
	peers := maps.Clone(p2p.activePeers)
	p2p.mu.Unlock()
	for _, p := range peers {
		if p.Closed.Load() {
			continue
		}
		err := p.Send(peerapi.Have(i))
		if err != nil {
			return err
		}
	}
	return nil
}

func (p2p *Server) queuePeer(peer *Peer) bool {
	if peer.Closed.Load() || peer.RemoteChoked.Load() {
		return false
	}
	if _, ok := p2p.queuedPeers[peer]; ok {
		return false
	}
	p2p.queuedPeers[peer] = struct{}{}
	p2p.downloadQueue <- peer
	return true
}

func (p2p *Server) disconnect(peer *Peer) error {
	for _, i := range peer.Bitfield.Items() {
		delete(p2p.indexToPeers[i], peer)
	}
	for addr := range p2p.knownAddrs[peer.IDIP()] {
		if idip, ok := p2p.knownIDIPs[addr]; ok && idip == peer.IDIP() {
			delete(p2p.knownIDIPs, addr)
		}
	}
	delete(p2p.knownAddrs, peer.IDIP())
	delete(p2p.activePeers, peer.IDIP())
	return peer.Close()
}

func (p2p *Server) newPeer(conn net.Conn) *Peer {
	peer := &Peer{
		Bitfield:         bitfield.New(len(p2p.Info.Pieces)),
		server:           p2p,
		Conn:             conn,
		inflightRequests: make(map[blockRequest]*future.Future[error]),
		canceledRequests: make(map[blockRequest]struct{}),
	}
	peer.RemoteChoked.Store(true)
	peer.Choked.Store(true)
	return peer
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

func remoteAddr(tp *tracker.Peer) (addr, error) {
	ip := net.ParseIP(tp.IP)
	if ip == nil {
		return "", errors.New("p2p: bad ip")
	}
	return addr(fmt.Sprintf("%s:%d", ip.String(), tp.Port)), nil
}
