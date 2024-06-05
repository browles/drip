package peer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

func ReadHandshake(r io.Reader) (*Handshake, error) {
	data := make([]byte, HandshakeLength)
	if _, err := r.Read(data[:1]); err != nil {
		return nil, err
	}
	if int(data[0]) != len(HandshakeProtocol) {
		return nil, errors.New("unsupported handshake protocol")
	}
	if _, err := io.ReadFull(r, data[1:]); err != nil {
		return nil, err
	}
	var handshake Handshake
	if err := handshake.UnmarshalBinary(data); err != nil {
		return nil, err
	}
	return &handshake, nil
}

func Read(r io.Reader) (*Message, error) {
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(r, lengthBytes); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lengthBytes)
	if length == 0 {
		return Keepalive, nil
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	var message Message
	if err := message.UnmarshalBinary(data); err != nil {
		return nil, err
	}
	return &message, nil
}

func Write(w io.Writer, bm BinaryMarshaler) (int, error) {
	bs, err := bm.MarshalBinary()
	if err != nil {
		return 0, err
	}
	return w.Write(bs)
}

type BinaryMarshaler interface {
	MarshalBinary() ([]byte, error)
	UnmarshalBinary(data []byte) error
}

const (
	HandshakeProtocol = "BitTorrent protocol"
	HandshakeLength   = 1 + len(HandshakeProtocol) + 8 + 20 + 20
)

type Handshake struct {
	InfoHash [20]byte
	PeerID   [20]byte
}

func (p *Handshake) MarshalBinary() ([]byte, error) {
	b := make([]byte, HandshakeLength)
	b[0] = byte(len(HandshakeProtocol))
	curr := 1
	curr += copy(b[curr:curr+len(HandshakeProtocol)], HandshakeProtocol)
	curr += copy(b[curr:curr+8], make([]byte, 8))
	curr += copy(b[curr:curr+20], p.InfoHash[:])
	curr += copy(b[curr:curr+20], p.PeerID[:])
	return b, nil
}

func (p *Handshake) UnmarshalBinary(data []byte) error {
	protocolLen := data[0]
	protocol := data[1 : 1+protocolLen]
	if string(protocol) != HandshakeProtocol {
		return fmt.Errorf("unsupported handshake protocol: %s", protocol)
	}
	curr := 1 + int(protocolLen) + 8
	curr += copy(p.InfoHash[:], data[curr:curr+20])
	curr += copy(p.PeerID[:], data[curr:curr+20])
	return nil
}

type MessageType uint8

const (
	Choke MessageType = iota
	Unchoke
	Interested
	NotInterested
	Have
	Bitfield
	Request
	Piece
	Cancel
)

var Keepalive = (*Message)(nil)

type Message struct {
	Type    MessageType
	Payload []byte
}

func (p *Message) MarshalBinary() ([]byte, error) {
	if p == Keepalive {
		return make([]byte, 4), nil
	}
	b := make([]byte, len(p.Payload)+5)
	binary.BigEndian.PutUint32(b[:4], uint32(len(p.Payload)+1))
	b[4] = byte(p.Type)
	copy(b[5:], p.Payload)
	return b, nil
}

func (p *Message) UnmarshalBinary(data []byte) error {
	length := binary.BigEndian.Uint32(data[:4])
	if int(length) != len(data)-4 {
		return fmt.Errorf("length mismatch: %d != %d", length, len(data)-4)
	}
	p.Type = MessageType(data[4])
	p.Payload = data[5:]
	return nil
}
