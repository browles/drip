package tracker

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func byte20(str string) [20]byte {
	var res [20]byte
	copy(res[:], str)
	return res
}

func TestRequest_Encode(t *testing.T) {
	tests := []struct {
		name string
		r    *Request
		want string
	}{
		{
			"tracker",
			&Request{
				InfoHash:   [20]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
				PeerID:     byte20("-peer-id-11111111111"),
				IP:         "1.2.3.4",
				Port:       6888,
				Uploaded:   1,
				Downloaded: 2,
				Left:       3,
				Event:      "started",
				Compact:    true,
			},
			fmt.Sprintf("compact=%s&downloaded=%s&event=%s&info_hash=%s&ip=%s&left=%s&peer_id=%s&port=%s&uploaded=%s",
				url.QueryEscape("1"),
				url.QueryEscape("2"),
				url.QueryEscape("started"),
				url.QueryEscape(string([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9})),
				url.QueryEscape("1.2.3.4"),
				url.QueryEscape("3"),
				url.QueryEscape("-peer-id-11111111111"),
				url.QueryEscape("6888"),
				url.QueryEscape("1"),
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.Encode()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Request.Encode() -want, +got\n%s", diff)
			}
		})
	}
}

func TestRequest_Decode(t *testing.T) {
	tests := []struct {
		name     string
		rawQuery string
		want     *Request
		wantErr  bool
	}{
		{
			"tracker",
			fmt.Sprintf("compact=%s&downloaded=%s&event=%s&info_hash=%s&ip=%s&left=%s&peer_id=%s&port=%s&uploaded=%s",
				url.QueryEscape("1"),
				url.QueryEscape("2"),
				url.QueryEscape("started"),
				url.QueryEscape(string([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9})),
				url.QueryEscape("1.2.3.4"),
				url.QueryEscape("3"),
				url.QueryEscape("-peer-id-11111111111"),
				url.QueryEscape("6888"),
				url.QueryEscape("1"),
			),
			&Request{
				InfoHash:   [20]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
				PeerID:     byte20("-peer-id-11111111111"),
				IP:         "1.2.3.4",
				Port:       6888,
				Uploaded:   1,
				Downloaded: 2,
				Left:       3,
				Event:      "started",
				Compact:    true,
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &Request{}
			err := got.Decode(tt.rawQuery)
			if (err != nil) != tt.wantErr {
				t.Errorf("Request.Decode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Request.Decode() -want, +got\n%s", diff)
			}
		})
	}
}

func TestPeers_MarshalBencoding(t *testing.T) {
	tests := []struct {
		name    string
		p       *Peers
		want    []byte
		wantErr bool
	}{
		{
			"compact",
			&Peers{
				Compact: true,
				List: []*Peer{
					{
						ID:   "abc",
						IP:   "1.2.3.4",
						Port: 5678,
					},
					{
						ID:   "def",
						IP:   "2.3.4.1",
						Port: 6785,
					},
				},
			},
			[]byte("12:" + string([]byte{1, 2, 3, 4, 0x16, 0x2e, 2, 3, 4, 1, 0x1a, 0x81})),
			false,
		},
		{
			"noncompact",
			&Peers{
				Compact: false,
				List: []*Peer{
					{
						ID:   "abc",
						IP:   "1.2.3.4",
						Port: 5678,
					},
					{
						ID:   "def",
						IP:   "2.3.4.1",
						Port: 6785,
					},
				},
			},
			[]byte("ld2:ip7:1.2.3.47:peer id3:abc4:porti5678eed2:ip7:2.3.4.17:peer id3:def4:porti6785eee"),
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
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Peers.MarshalBencoding() -want, +got\n%s", diff)
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
						ID:   "abc",
						IP:   "1.2.3.4",
						Port: 5678,
					},
					{
						ID:   "def",
						IP:   "2.3.4.1",
						Port: 6785,
					},
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &Peers{}
			err := got.UnmarshalBencoding([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Errorf("Peers.UnmarshalBencoding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Peers.UnmarshalBencoding() -want, +got\n%s", diff)
			}
		})
	}
}
