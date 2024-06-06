package peer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

func ReadHandshake(r io.Reader) (*Handshake, error) {
	var handshake Handshake
	if err := handshake.Decode(r); err != nil {
		return nil, err
	}
	return &handshake, nil
}

func ReadMessage(r io.Reader) (*Message, error) {
	var message Message
	if err := message.Decode(r); err != nil {
		return nil, err
	}
	return &message, nil
}

func Write(w io.Writer, e encoder) error {
	return e.Encode(w)
}

type encoder interface {
	Encode(w io.Writer) error
}

const (
	HandshakeProtocol = "BitTorrent protocol"
	HandshakeLength   = 1 + len(HandshakeProtocol) + 8 + 20 + 20
)

type Handshake struct {
	InfoHash [20]byte
	PeerID   [20]byte
}

func (p *Handshake) Encode(w io.Writer) error {
	data := make([]byte, HandshakeLength)
	data[0] = byte(len(HandshakeProtocol))
	curr := 1
	curr += copy(data[curr:curr+len(HandshakeProtocol)], HandshakeProtocol)
	curr += copy(data[curr:curr+8], make([]byte, 8))
	curr += copy(data[curr:curr+20], p.InfoHash[:])
	curr += copy(data[curr:curr+20], p.PeerID[:])
	_, err := w.Write(data)
	return err
}

func (p *Handshake) MarshalBinary() ([]byte, error) {
	var bu bytes.Buffer
	if err := p.Encode(&bu); err != nil {
		return nil, err
	}
	return bu.Bytes(), nil
}

func (p *Handshake) Decode(r io.Reader) error {
	data := make([]byte, HandshakeLength)
	if _, err := r.Read(data[:1]); err != nil {
		return err
	}
	protocolLen := int(data[0])
	if protocolLen != len(HandshakeProtocol) {
		return errors.New("unsupported handshake protocol")
	}
	if _, err := io.ReadFull(r, data[1:]); err != nil {
		return err
	}
	protocol := data[1 : 1+protocolLen]
	if string(protocol) != HandshakeProtocol {
		return fmt.Errorf("unsupported handshake protocol: %s", protocol)
	}
	curr := 1 + int(protocolLen) + 8
	curr += copy(p.InfoHash[:], data[curr:curr+20])
	curr += copy(p.PeerID[:], data[curr:curr+20])
	return nil
}

func (p *Handshake) UnmarshalBinary(data []byte) error {
	return p.Decode(bytes.NewReader(data))
}

type MessageType int

const (
	// Non-standard, so use an unused int
	KEEPALIVE             = MessageType(-1)
	CHOKE     MessageType = iota
	UNCHOKE
	INTERESTED
	NOT_INTERESTED
	HAVE
	BITFIELD
	REQUEST
	PIECE
	CANCEL
)

type Message struct {
	Type     MessageType
	Bitfield Bitfield
	Index    int
	Begin    int
	Length   int
	Piece    []byte
}

func (p *Message) Encode(w io.Writer) error {
	var fixedData, variableData []byte
	switch p.Type {
	case KEEPALIVE:
		fixedData = make([]byte, 4)
	case CHOKE, UNCHOKE, INTERESTED, NOT_INTERESTED:
		fixedData = make([]byte, 4+1)
		binary.BigEndian.PutUint32(fixedData[0:4], 1)
		fixedData[4] = byte(p.Type)
	case BITFIELD:
		fixedData = make([]byte, 4+1)
		binary.BigEndian.PutUint32(fixedData[0:4], uint32(1+len(p.Bitfield)))
		fixedData[4] = byte(p.Type)
		variableData = p.Bitfield
	case HAVE:
		fixedData = make([]byte, 4+1+4)
		binary.BigEndian.PutUint32(fixedData[0:4], uint32(1+4))
		fixedData[4] = byte(p.Type)
		binary.BigEndian.PutUint32(fixedData[5:9], uint32(p.Index))
	case REQUEST, CANCEL:
		fixedData = make([]byte, 4+1+12)
		binary.BigEndian.PutUint32(fixedData[0:4], uint32(1+12))
		fixedData[4] = byte(p.Type)
		binary.BigEndian.PutUint32(fixedData[5:9], uint32(p.Index))
		binary.BigEndian.PutUint32(fixedData[9:13], uint32(p.Begin))
		binary.BigEndian.PutUint32(fixedData[13:17], uint32(p.Length))
	case PIECE:
		fixedData = make([]byte, 4+1+8)
		binary.BigEndian.PutUint32(fixedData[0:4], uint32(1+8+len(p.Piece)))
		fixedData[4] = byte(p.Type)
		binary.BigEndian.PutUint32(fixedData[5:9], uint32(p.Index))
		binary.BigEndian.PutUint32(fixedData[9:13], uint32(p.Begin))
		variableData = p.Piece
	}
	if _, err := w.Write(fixedData); err != nil {
		return err
	}
	if _, err := w.Write(variableData); err != nil {
		return err
	}
	return nil
}

func (p *Message) MarshalBinary() ([]byte, error) {
	var bu bytes.Buffer
	err := p.Encode(&bu)
	if err != nil {
		return nil, err
	}
	return bu.Bytes(), nil
}

func (p *Message) Decode(r io.Reader) error {
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(r, lengthBytes); err != nil {
		return err
	}
	length := binary.BigEndian.Uint32(lengthBytes)
	if length == 0 {
		p.Type = KEEPALIVE
		return nil
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return err
	}
	p.Type = MessageType(payload[0])
	payload = payload[1:]
	switch p.Type {
	case CHOKE, UNCHOKE, INTERESTED, NOT_INTERESTED:
	case BITFIELD:
		p.Bitfield = payload
	case HAVE:
		p.Index = int(binary.BigEndian.Uint32(payload[0:4]))
	case REQUEST, CANCEL:
		p.Index = int(binary.BigEndian.Uint32(payload[0:4]))
		p.Begin = int(binary.BigEndian.Uint32(payload[4:8]))
		p.Length = int(binary.BigEndian.Uint32(payload[8:12]))
	case PIECE:
		p.Index = int(binary.BigEndian.Uint32(payload[0:4]))
		p.Begin = int(binary.BigEndian.Uint32(payload[4:8]))
		p.Piece = payload[8:]
	}
	return nil
}

func (p *Message) UnmarshalBinary(data []byte) error {
	return p.Decode(bytes.NewReader(data))
}
