package p2p

import (
	"context"
	"errors"
	"fmt"
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
	Pieces           bitfield.Bitfield

	mu               sync.Mutex
	inflightRequests map[blockRequest]chan error
}

var ErrChoked = errors.New("p2p: peer connection is choked")

func DialTCP(peer *tracker.Peer) (*Peer, error) {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", peer.IP, peer.Port))
	if err != nil {
		return nil, fmt.Errorf("DialTCP: %w", err)
	}
	return &Peer{
		Conn:         conn,
		ID:           peer.ID,
		RemoteChoked: true,
		Choked:       true,
	}, nil
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
		return errors.New("Handshake: info hashes do not match")
	}
	if p.ID != "" && p.ID != string(phs.PeerID[:]) {
		return errors.New("Handshake: peer IDs do not match")
	}
	p.ID = string(phs.PeerID[:])
	return nil
}

func (p *Peer) Send(m *peerapi.Message) error {
	p.SetReadDeadline(time.Now().Add(2 * time.Minute))
	return peerapi.Write(p.Conn, m)
}

func (p *Peer) Receive() (*peerapi.Message, error) {
	p.SetReadDeadline(time.Now().Add(2 * time.Minute))
	m, err := peerapi.ReadMessage(p.Conn)
	if err != nil {
		return nil, err
	}
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

func (p *Peer) completeRequest(br *blockRequest, err error) {
	if done, ok := p.inflightRequests[*br]; ok {
		if err != nil {
			done <- err
		}
		close(done)
		delete(p.inflightRequests, *br)
	}
}

func (p *Peer) requestBlock(ctx context.Context, br *blockRequest) error {
	if p.RemoteChoked {
		return ErrChoked
	}
	p.mu.Lock()
	if _, ok := p.inflightRequests[*br]; ok {
		p.mu.Unlock()
		return nil
	}
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
