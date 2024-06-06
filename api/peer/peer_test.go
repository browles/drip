package peer

import (
	"reflect"
	"slices"
	"testing"
)

func TestHandshake_MarshalBinary(t *testing.T) {
	testHandshake := &Handshake{
		InfoHash: [20]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		PeerID:   [20]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	}
	copy(testHandshake.InfoHash[:], []byte("SHA1-HASH-abcdefghij"))
	copy(testHandshake.PeerID[:], []byte("-DR1000-123456789012"))
	tests := []struct {
		name    string
		h       *Handshake
		want    []byte
		wantErr bool
	}{
		{
			"marshal",
			testHandshake,
			slices.Concat(
				[]byte{19},
				[]byte("BitTorrent protocol"),
				make([]byte, 8),
				testHandshake.InfoHash[:],
				testHandshake.PeerID[:],
			),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.h.MarshalBinary()
			if (err != nil) != tt.wantErr {
				t.Errorf("Handshake.MarshalBinary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Handshake.MarshalBinary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandshake_UnmarshalBinary(t *testing.T) {
	testHandshake := &Handshake{
		InfoHash: [20]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		PeerID:   [20]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	}
	copy(testHandshake.InfoHash[:], []byte("SHA1-HASH-abcdefghij"))
	copy(testHandshake.PeerID[:], []byte("-DR1000-123456789012"))
	tests := []struct {
		name    string
		data    []byte
		want    *Handshake
		wantErr bool
	}{
		{
			"marshal",
			slices.Concat(
				[]byte{19},
				[]byte("BitTorrent protocol"),
				make([]byte, 8),
				testHandshake.InfoHash[:],
				testHandshake.PeerID[:],
			),
			testHandshake,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handshake{}
			err := h.UnmarshalBinary(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Handshake.UnmarshalBinary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(h, tt.want) {
				t.Errorf("Handshake.UnmarshalBinary() = %+v, want %+v", h, tt.want)
			}
		})
	}
}

func TestMessage_MarshalBinary(t *testing.T) {
	tests := []struct {
		name    string
		m       *Message
		want    []byte
		wantErr bool
	}{
		{
			"keepalive",
			&Message{
				Type: KEEPALIVE,
			},
			[]byte{0, 0, 0, 0},
			false,
		},
		{
			"choke",
			&Message{
				Type: CHOKE,
			},
			[]byte{0, 0, 0, 1, byte(CHOKE)},
			false,
		},
		{
			"unchoke",
			&Message{
				Type: UNCHOKE,
			},
			[]byte{0, 0, 0, 1, byte(UNCHOKE)},
			false,
		},
		{
			"interested",
			&Message{
				Type: INTERESTED,
			},
			[]byte{0, 0, 0, 1, byte(INTERESTED)},
			false,
		},
		{
			"not interested",
			&Message{
				Type: NOT_INTERESTED,
			},
			[]byte{0, 0, 0, 1, byte(NOT_INTERESTED)},
			false,
		},
		{
			"bitfield",
			&Message{
				Type:     BITFIELD,
				Bitfield: Bitfield{0b01010101, 0b11001100},
			},
			[]byte{0, 0, 0, 3, byte(BITFIELD), 0b01010101, 0b11001100},
			false,
		},
		{
			"have",
			&Message{
				Type:  HAVE,
				Index: 0x01020304,
			},
			[]byte{0, 0, 0, 5, byte(HAVE), 0x01, 0x02, 0x03, 0x04},
			false,
		},
		{
			"request",
			&Message{
				Type:   REQUEST,
				Index:  0x01020304,
				Begin:  0x11223344,
				Length: 0x55667788,
			},
			[]byte{
				0, 0, 0, 13, byte(REQUEST),
				0x01, 0x02, 0x03, 0x04,
				0x11, 0x22, 0x33, 0x44,
				0x55, 0x66, 0x77, 0x88,
			},
			false,
		},
		{
			"cancel",
			&Message{
				Type:   CANCEL,
				Index:  0x01020304,
				Begin:  0x11223344,
				Length: 0x55667788,
			},
			[]byte{
				0, 0, 0, 13, byte(CANCEL),
				0x01, 0x02, 0x03, 0x04,
				0x11, 0x22, 0x33, 0x44,
				0x55, 0x66, 0x77, 0x88,
			},
			false,
		},
		{
			"piece",
			&Message{
				Type:  PIECE,
				Index: 0x01020304,
				Begin: 0x11223344,
				Piece: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9},
			},
			[]byte{
				0, 0, 0, 18, byte(PIECE),
				0x01, 0x02, 0x03, 0x04,
				0x11, 0x22, 0x33, 0x44,
				1, 2, 3, 4, 5, 6, 7, 8, 9,
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.m.MarshalBinary()
			if (err != nil) != tt.wantErr {
				t.Errorf("Message.MarshalBinary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Message.MarshalBinary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessage_UnmarshalBinary(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    *Message
		wantErr bool
	}{
		{
			"keepalive",
			[]byte{0, 0, 0, 0},
			&Message{
				Type: KEEPALIVE,
			},
			false,
		},
		{
			"choke",
			[]byte{0, 0, 0, 1, byte(CHOKE)},
			&Message{
				Type: CHOKE,
			},
			false,
		},
		{
			"unchoke",
			[]byte{0, 0, 0, 1, byte(UNCHOKE)},
			&Message{
				Type: UNCHOKE,
			},
			false,
		},
		{
			"interested",
			[]byte{0, 0, 0, 1, byte(INTERESTED)},
			&Message{
				Type: INTERESTED,
			},
			false,
		},
		{
			"not interested",
			[]byte{0, 0, 0, 1, byte(NOT_INTERESTED)},
			&Message{
				Type: NOT_INTERESTED,
			},
			false,
		},
		{
			"bitfield",
			[]byte{0, 0, 0, 3, byte(BITFIELD), 0b01010101, 0b11001100},
			&Message{
				Type:     BITFIELD,
				Bitfield: Bitfield{0b01010101, 0b11001100},
			},
			false,
		},
		{
			"have",
			[]byte{0, 0, 0, 5, byte(HAVE), 0x01, 0x02, 0x03, 0x04},
			&Message{
				Type:  HAVE,
				Index: 0x01020304,
			},
			false,
		},
		{
			"request",
			[]byte{
				0, 0, 0, 13, byte(REQUEST),
				0x01, 0x02, 0x03, 0x04,
				0x11, 0x22, 0x33, 0x44,
				0x55, 0x66, 0x77, 0x88,
			},
			&Message{
				Type:   REQUEST,
				Index:  0x01020304,
				Begin:  0x11223344,
				Length: 0x55667788,
			},
			false,
		},
		{
			"cancel",
			[]byte{
				0, 0, 0, 13, byte(CANCEL),
				0x01, 0x02, 0x03, 0x04,
				0x11, 0x22, 0x33, 0x44,
				0x55, 0x66, 0x77, 0x88,
			},
			&Message{
				Type:   CANCEL,
				Index:  0x01020304,
				Begin:  0x11223344,
				Length: 0x55667788,
			},
			false,
		},
		{
			"piece",
			[]byte{
				0, 0, 0, 18, byte(PIECE),
				0x01, 0x02, 0x03, 0x04,
				0x11, 0x22, 0x33, 0x44,
				1, 2, 3, 4, 5, 6, 7, 8, 9,
			},
			&Message{
				Type:  PIECE,
				Index: 0x01020304,
				Begin: 0x11223344,
				Piece: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Message{}
			err := m.UnmarshalBinary(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Message.UnmarshalBinary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(m, tt.want) {
				t.Errorf("Message.UnmarshalBinary() = %v, want %v", m, tt.want)
			}
		})
	}
}
