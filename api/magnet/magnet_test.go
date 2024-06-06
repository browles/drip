package magnet

import (
	"reflect"
	"testing"
)

func TestParseMagnetURL(t *testing.T) {
	tests := []struct {
		name    string
		link    string
		want    *Magnet
		wantErr bool
	}{
		{
			"valid",
			"magnet:?xt=urn:btih:aaaaaaaabbbbbbbbccccccccddddddddeeeeeeee&dn=torrent_name&xl=123456&tr=udp%3A%2F%2Ftracker.example.com%3A1337&tr=udp%3A%2F%2Ftracker2.example.com%3A1337&ws=http%3A%2F%2Fws.example.com&x.pe=1.2.3.4%3A6688",
			&Magnet{
				ExactTopic: "urn:btih:aaaaaaaabbbbbbbbccccccccddddddddeeeeeeee",
				Name:       "torrent_name",
				Length:     123456,
				Trackers:   []string{"udp://tracker.example.com:1337", "udp://tracker2.example.com:1337"},
				WebSeed:    "http://ws.example.com",
				Peers:      []string{"1.2.3.4:6688"},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMagnetURL(tt.link)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMagnetURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseMagnetURL() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
