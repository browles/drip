package tracker

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/browles/drip/bencode"
)

type Request struct {
	Announce   string
	InfoHash   [20]byte
	PeerID     string
	IP         string
	Port       int
	Uploaded   int
	Downloaded int
	Left       int
	Event      string
	Compact    bool
	// more?
}

func ifelse[T any](t bool, a, b T) T {
	if t {
		return a
	}
	return b
}

func (t *Request) URL() *url.URL {
	params := make(url.Values)
	for k, v := range map[string]string{
		// http://bittorrent.org/beps/bep_0003.html
		"info_hash":  string(t.InfoHash[:]),
		"peer_id":    t.PeerID,
		"ip":         t.IP,
		"port":       strconv.Itoa(t.Port),
		"uploaded":   strconv.Itoa(t.Uploaded),
		"downloaded": strconv.Itoa(t.Downloaded),
		"left":       strconv.Itoa(t.Left),
		"event":      t.Event,
		// http://bittorrent.org/beps/bep_0023.html
		"compact": ifelse(t.Compact, "1", "0"),
		// ?
		"no_peer_id": "",
		"numwant":    "",
		"key":        "",
		"trackerid":  "",
	} {
		if v != "" {
			params.Set(k, v)
		}
	}
	url, err := url.Parse(t.Announce)
	if err != nil {
		return nil
	}
	url.RawQuery = params.Encode()
	return url
}

type Response struct {
	// http://bittorrent.org/beps/bep_0003.html
	FailureReason string `bencode:"failure reason,omitempty"`
	Interval      int    `bencode:"interval"`
	Peers         *Peers `bencode:"peers"`
	// ?
	WarningReason string `bencode:"warning reason,omitempty"`
	MinInterval   int    `bencode:"min interval,omitempty"`
	TrackerID     string `bencode:"tracker id"`
	Complete      int    `bencode:"complete"`
	Incomplete    int    `bencode:"incomplete"`
}

type Peers struct {
	Compact bool
	List    []*Peer
}

func (p *Peers) MarshalBencoding() ([]byte, error) {
	if p.Compact {
		compact, err := compactPeers(p.List)
		if err != nil {
			return nil, err
		}
		return bencode.Marshal(compact)
	}
	return bencode.Marshal(p.List)
}

func compactPeers(peers []*Peer) ([]byte, error) {
	b := make([]byte, 6*len(peers))
	i := 0
	for _, p := range peers {
		ip := net.ParseIP(p.IP).To4()
		if ip == nil {
			return nil, fmt.Errorf(`peer "ip" is not an ipv4: %+v`, p.IP)
		}
		b[i] = ip[0]
		b[i+1] = ip[1]
		b[i+2] = ip[2]
		b[i+3] = ip[3]
		binary.BigEndian.PutUint16(b[i+4:], uint16(p.Port))
		i += 6
	}
	return b, nil
}

func (p *Peers) UnmarshalBencoding(data []byte) error {
	var v any
	err := bencode.Unmarshal(data, &v)
	if err != nil {
		return err
	}
	switch v := v.(type) {
	case string:
		p.Compact = true
		p.List, err = parseCompactPeers(v)
	case []any:
		p.List, err = parsePeers(v)
	default:
		return errors.New("unknown peers format")
	}
	return err
}

func parseCompactPeers(compact string) ([]*Peer, error) {
	if len(compact)%6 != 0 {
		return nil, errors.New("compact peers list length not multiple of 6")
	}
	var peers []*Peer
	for i := 0; i < len(compact); i += 6 {
		ip := net.IPv4(compact[i], compact[i+1], compact[i+2], compact[i+3])
		port := binary.BigEndian.Uint16([]byte(compact[i+4 : i+6]))
		peer := &Peer{
			IP:   ip.String(),
			Port: int(port),
		}
		peers = append(peers, peer)
	}
	return peers, nil
}

func parsePeers(peerMaps []any) ([]*Peer, error) {
	var peers []*Peer
	for _, m := range peerMaps {
		m := m.(map[string]any)
		peer := &Peer{
			PeerID: m["peer id"].(string),
			IP:     m["ip"].(string),
			Port:   m["port"].(int),
		}
		peers = append(peers, peer)
	}
	return peers, nil
}

type Peer struct {
	PeerID string `bencode:"peer id"`
	IP     string `bencode:"ip"`
	Port   int    `bencode:"port"`
}
