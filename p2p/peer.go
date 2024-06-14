package p2p

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/browles/drip/api/peer"
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
}

func DialTCP(peer *tracker.Peer) (*Peer, error) {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", peer.IP, peer.Port))
	if err != nil {
		return nil, fmt.Errorf("DialTCP: %w", err)
	}
	return &Peer{Conn: conn, ID: peer.ID}, nil
}

func (p *Peer) Handshake(hs *peer.Handshake) error {
	if err := peer.Write(p.Conn, hs); err != nil {
		return err
	}
	phs, err := peer.ReadHandshake(p.Conn)
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

func (p *Peer) Receive() (*peer.Message, error) {
	return peer.ReadMessage(p.Conn)
}

func (p *Peer) Send(m *peer.Message) error {
	return peer.Write(p.Conn, m)
}

func (p *Peer) Keepalive() error {
	if err := p.SetDeadline(time.Now().Add(2 * time.Minute)); err != nil {
		return err
	}
	return p.Send(&peer.Message{Type: peer.KEEPALIVE})
}
