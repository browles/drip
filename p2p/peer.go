package p2p

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	peerapi "github.com/browles/drip/api/peer"
	"github.com/browles/drip/api/tracker"
	"github.com/browles/drip/bitfield"
)

type Peer struct {
	net.Conn
	ID               string
	RemoteChoked     bool
	RemoteInterested bool
	Choked           bool
	Bitfield         bitfield.Bitfield

	mu               sync.Mutex
	inflightRequests map[blockRequest]chan error
}

var ErrChoked = errors.New("p2p: peer connection is choked")

func DialTCP(peer *tracker.Peer) (*Peer, error) {
	slog.Debug("DialTCP", "ip", peer.IP, "port", peer.Port)
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", peer.IP, peer.Port))
	if err != nil {
		return nil, fmt.Errorf("DialTCP: %w", err)
	}
	return &Peer{
		Conn:             conn,
		ID:               peer.ID,
		RemoteChoked:     true,
		Choked:           true,
		inflightRequests: map[blockRequest]chan error{},
	}, nil
}

func FromConn(conn net.Conn) *Peer {
	return &Peer{
		Conn:             conn,
		RemoteChoked:     true,
		Choked:           true,
		inflightRequests: map[blockRequest]chan error{},
	}
}

func (p *Peer) Handshake(hs *peerapi.Handshake) error {
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
	if p.ID != "" && p.ID != string(phs.PeerID[:]) {
		return errors.New("p2p: Handshake: peer IDs do not match")
	}
	p.ID = string(phs.PeerID[:])
	slog.Debug("Handshake", "id", p.ID)
	return nil
}

func (p *Peer) Send(m *peerapi.Message) error {
	defer slog.Debug("Send", "peer", p.ID, "message", m)
	p.SetReadDeadline(time.Now().Add(2 * time.Minute))
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
	return peerapi.Write(p.Conn, peerapi.Keepalive())
}

func (p *Peer) Choke() error {
	p.Choked = true
	return peerapi.Write(p.Conn, peerapi.Choke())
}

func (p *Peer) Unchoke() error {
	p.Choked = false
	return peerapi.Write(p.Conn, peerapi.Unchoke())
}

func (p *Peer) RequestBlock(ctx context.Context, br *blockRequest) error {
	if p.RemoteChoked {
		return ErrChoked
	}
	p.mu.Lock()
	if _, ok := p.inflightRequests[*br]; ok {
		p.mu.Unlock()
		return nil
	}
	start := time.Now()
	slog.Debug("RequestBlock", "index", br.index, "begin", br.begin, "length", br.length)
	defer func() {
		slog.Debug("RequestBlock.time", "index", br.index, "begin", br.begin, "length", br.length, "time", time.Since(start))
	}()
	done := make(chan error)
	p.inflightRequests[*br] = done
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
	case err := <-done:
		return err
	}
}

func (p *Peer) completeRequest(br *blockRequest, err error) {
	slog.Debug("completeRequest", "index", br.index, "begin", br.begin, "length", br.length, "err", err)
	if done, ok := p.inflightRequests[*br]; ok {
		if err != nil {
			done <- err
		}
		close(done)
		delete(p.inflightRequests, *br)
	}
}
