package peer

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	handshakeHeader       = "BitTorrent protocol"
	handshakeLengthPrefix = byte(len(handshakeHeader))
)

type Handshake struct {
	InfoHash [20]byte
	PeerID   [20]byte
}

func (p *Handshake) MarshalBinary() ([]byte, error) {
	var bu bytes.Buffer
	bu.WriteByte(handshakeLengthPrefix)
	bu.Write([]byte(handshakeHeader))
	bu.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0})
	bu.Write(p.InfoHash[:])
	bu.Write(p.PeerID[:])
	return bu.Bytes(), nil
}

func (p *Handshake) UnmarshalBinary(data []byte) error {
	headerLen := data[0]
	header := data[1 : 1+headerLen]
	if string(header) != handshakeHeader {
		return fmt.Errorf("unknown protocol: %s", header)
	}
	infoOffset := 1 + headerLen + 8
	copy(p.InfoHash[:], data[infoOffset:infoOffset+20])
	copy(p.PeerID[:], data[infoOffset+20:infoOffset+40])
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

var Keepalive = []byte{0, 0, 0, 0}

type Message struct {
	Type    MessageType
	Payload []byte
}

func (p *Message) MarshalBinary() []byte {
	b := make([]byte, len(p.Payload)+5)
	binary.BigEndian.PutUint32(b[:4], uint32(len(p.Payload)+1))
	b[4] = byte(p.Type)
	copy(b[5:], p.Payload)
	return b
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
