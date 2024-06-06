package tracker

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"
)

func TestRequest_URL(t *testing.T) {
	tests := []struct {
		name string
		r    *Request
		want string
	}{
		{
			"tracker",
			&Request{
				Announce:   "http://example.com",
				InfoHash:   [20]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
				PeerID:     "-peer-id-",
				IP:         "1.2.3.4",
				Port:       6888,
				Uploaded:   1,
				Downloaded: 2,
				Left:       3,
				Event:      "started",
				Compact:    true,
			},
			fmt.Sprintf("http://example.com?compact=%s&downloaded=%s&event=%s&info_hash=%s&ip=%s&left=%s&peer_id=%s&port=%s&uploaded=%s",
				url.QueryEscape("1"),
				url.QueryEscape("2"),
				url.QueryEscape("started"),
				url.QueryEscape(string([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9})),
				url.QueryEscape("1.2.3.4"),
				url.QueryEscape("3"),
				url.QueryEscape("-peer-id-"),
				url.QueryEscape("6888"),
				url.QueryEscape("1"),
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.URL().String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Request.URL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPeers_MarshalBencoding(t *testing.T) {
	tests := []struct {
		name    string
		p       *Peers
		want    string
		wantErr bool
	}{
		{
			"compact",
			&Peers{
				Compact: true,
				List: []*Peer{
					{
						PeerID: "abc",
						IP:     "1.2.3.4",
						Port:   5678,
					},
					{
						PeerID: "def",
						IP:     "2.3.4.1",
						Port:   6785,
					},
				},
			},
			"12:" + string([]byte{1, 2, 3, 4, 0x16, 0x2e, 2, 3, 4, 1, 0x1a, 0x81}),
			false,
		},
		{
			"noncompact",
			&Peers{
				Compact: false,
				List: []*Peer{
					{
						PeerID: "abc",
						IP:     "1.2.3.4",
						Port:   5678,
					},
					{
						PeerID: "def",
						IP:     "2.3.4.1",
						Port:   6785,
					},
				},
			},
			"ld2:ip7:1.2.3.47:peer id3:abc4:porti5678eed2:ip7:2.3.4.17:peer id3:def4:porti6785eee",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.MarshalBencoding()
			if (err != nil) != tt.wantErr {
				t.Errorf("Peers.MarshalBencoding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(string(got), tt.want) {
				t.Errorf("Peers.MarshalBencoding() = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func TestPeers_UnmarshalBencoding(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    *Peers
		wantErr bool
	}{
		{
			"compact",
			"12:" + string([]byte{1, 2, 3, 4, 0x16, 0x2e, 2, 3, 4, 1, 0x1a, 0x81}),
			&Peers{
				Compact: true,
				List: []*Peer{
					{
						IP:   "1.2.3.4",
						Port: 5678,
					},
					{
						IP:   "2.3.4.1",
						Port: 6785,
					},
				},
			},
			false,
		},
		{
			"noncompact",
			"ld2:ip7:1.2.3.47:peer id3:abc4:porti5678eed2:ip7:2.3.4.17:peer id3:def4:porti6785eee",
			&Peers{
				Compact: false,
				List: []*Peer{
					{
						PeerID: "abc",
						IP:     "1.2.3.4",
						Port:   5678,
					},
					{
						PeerID: "def",
						IP:     "2.3.4.1",
						Port:   6785,
					},
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Peers{}
			err := p.UnmarshalBencoding([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Errorf("Peers.UnmarshalBencoding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(p, tt.want) {
				t.Errorf("Peers.UnmarshalBencoding() = %v, want %v", p, tt.want)
			}
		})
	}
}
