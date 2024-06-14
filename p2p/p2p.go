package p2p

import (
	"context"
	"sync"
	"time"

	"github.com/browles/drip/api/metainfo"
	api "github.com/browles/drip/api/peer"
	"github.com/browles/drip/api/tracker"
	"github.com/browles/drip/storage"
)

type Manager struct {
	Info   *metainfo.Info
	PeerID [20]byte

	Storage *storage.Storage

	mu               sync.Mutex
	KnownPeers       map[tracker.Peer]struct{}
	ActivePeers      map[string]*Peer
	IndexToPeerIDs   map[int]map[string]struct{}
	CancelsToPeerIDs map[[3]int]map[string]struct{}

	OutgoingRequests chan *blockRequest
	IncomingPieces   chan *blockResponse
	IncomingRequests chan *blockRequest
}

type blockResponse struct {
	peer  *Peer
	index int
	begin int
	piece []byte
}

type blockRequest struct {
	peer   *Peer
	index  int
	begin  int
	length int
}

func New(info *metainfo.Info, peerID [20]byte, storage *storage.Storage) *Manager {
	return &Manager{
		Info:    info,
		PeerID:  peerID,
		Storage: storage,

		KnownPeers:       make(map[tracker.Peer]struct{}),
		ActivePeers:      make(map[string]*Peer),
		IndexToPeerIDs:   make(map[int]map[string]struct{}),
		CancelsToPeerIDs: make(map[[3]int]map[string]struct{}),

		OutgoingRequests: make(chan *blockRequest),
		IncomingPieces:   make(chan *blockResponse),
		IncomingRequests: make(chan *blockRequest),
	}
}

func (ma *Manager) AddPeer(p *tracker.Peer) {
	ma.mu.Lock()
	if _, ok := ma.KnownPeers[*p]; ok {
		return
	}
	defer ma.mu.Unlock()
	ma.KnownPeers[*p] = struct{}{}
}

func (ma *Manager) RemovePeer(p *tracker.Peer) {
	ma.mu.Lock()
	defer ma.mu.Unlock()
	delete(ma.KnownPeers, *p)
}

func (ma *Manager) UpdatePeers(peers []*tracker.Peer) {
	newPeers := make(map[tracker.Peer]struct{})
	for _, p := range peers {
		newPeers[*p] = struct{}{}
	}
	for p := range newPeers {
		if _, ok := ma.KnownPeers[p]; ok {
			continue
		}
		ma.AddPeer(&p)
	}
	for p := range ma.KnownPeers {
		if _, ok := newPeers[p]; !ok {
			ma.RemovePeer(&p)
		}
	}
}

func (ma *Manager) Start() error {
	err := ma.Storage.AddTorrent(ma.Info)
	if err != nil {
		return err
	}
	return nil
}

func (ma *Manager) RequestPiece(ctx context.Context, peer *Peer, index int) error {
	pieceLength := ma.Info.PieceLength
	if index == len(ma.Info.Pieces)-1 {
		pieceLength = ma.Info.Length - index*ma.Info.PieceLength
	}
	begin := 0
	for begin < pieceLength {
		blockLength := min(ma.Storage.BlockLength, pieceLength-begin)
		ma.OutgoingRequests <- &blockRequest{peer, index, begin, blockLength}
		begin += blockLength
	}
	storagePiece := ma.Storage.GetPiece(ma.Info.SHA1, index)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-storagePiece.Done:
	}
	ma.mu.Lock()
	defer ma.mu.Unlock()
	for _, p := range ma.ActivePeers {
		err := p.Send(api.Have(index))
		if err != nil {
			return err
		}
	}
	return nil
}

func (ma *Manager) Connect(ctx context.Context, p *tracker.Peer) error {
	peer, err := DialTCP(p)
	if err != nil {
		return err
	}
	hs := &api.Handshake{
		InfoHash: ma.Info.SHA1,
		PeerID:   ma.PeerID,
	}
	if err := peer.Handshake(hs); err != nil {
		return err
	}
	ma.ActivePeers[peer.ID] = peer
	torrent := ma.Storage.GetTorrent(ma.Info.SHA1)
	if err := peer.Send(api.Bitfield(torrent.Bitfield)); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			m, err := peer.Receive()
			if err != nil {
				return err
			}
			if err := ma.HandleMessage(peer, m); err != nil {
				return err
			}
		}
	}
}

func (ma *Manager) HandleMessage(peer *Peer, m *api.Message) error {
	switch m.Type {
	case api.KEEPALIVE:
		peer.SetReadDeadline(time.Now().Add(2 * time.Minute))
	case api.CHOKE:
		peer.RemoteChoked = true
	case api.UNCHOKE:
		peer.RemoteChoked = false
	case api.INTERESTED:
		peer.RemoteInterested = true
	case api.NOT_INTERESTED:
		peer.RemoteInterested = false
	case api.HAVE:
		ma.HandleHave(peer, m.Index())
	case api.BITFIELD:
		bf := m.Bitfield()
		for _, i := range bf.Items() {
			ma.HandleHave(peer, i)
		}
	case api.REQUEST:
		ma.HandleRequest(peer, m.Index(), m.Begin(), m.Length())
	case api.CANCEL:
		ma.HandleCancel(peer, m.Index(), m.Begin(), m.Length())
	case api.PIECE:
		ma.HandlePiece(peer, m.Index(), m.Begin(), m.Piece())
	}
	return nil
}

func (ma *Manager) HandleHave(peer *Peer, i int) {
	peer.Pieces.Add(i)
	ma.mu.Lock()
	defer ma.mu.Unlock()
	m, ok := ma.IndexToPeerIDs[i]
	if !ok {
		m = make(map[string]struct{})
		ma.IndexToPeerIDs[i] = m
	}
	m[peer.ID] = struct{}{}
}

func (ma *Manager) HandleRequest(peer *Peer, index, begin, length int) {
	if peer.Choked {
		// Peer is not allowed requests
		return
	}
	ma.mu.Lock()
	defer ma.mu.Unlock()
	k := [3]int{index, begin, length}
	m, ok := ma.CancelsToPeerIDs[k]
	if ok {
		delete(m, peer.ID)
	}
	ma.IncomingRequests <- &blockRequest{peer, index, begin, length}
}

func (ma *Manager) HandleCancel(peer *Peer, index, begin, length int) {
	ma.mu.Lock()
	defer ma.mu.Unlock()
	k := [3]int{index, begin, length}
	m, ok := ma.CancelsToPeerIDs[k]
	if !ok {
		m = make(map[string]struct{})
		ma.CancelsToPeerIDs[k] = m
	}
	m[peer.ID] = struct{}{}
}

func (ma *Manager) HandlePiece(peer *Peer, index, begin int, piece []byte) {
	ma.IncomingPieces <- &blockResponse{peer, index, begin, piece}
}
