package pex

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/browles/drip/api/metainfo"
	trackerapi "github.com/browles/drip/api/tracker"
	"github.com/browles/drip/bencode"
)

type HTTP struct {
	Port     int
	Torrents map[[20]byte]*Torrent
}

type Torrent struct {
	Info *metainfo.Info

	mu    sync.Mutex
	Peers map[string]*Peer
}

type Peer struct {
	tp        *trackerapi.Peer
	UpdatedAt time.Time
	LastEvent string
}

func NewHTTP(infos []*metainfo.Info, port int) *HTTP {
	t := &HTTP{
		Port:     port,
		Torrents: make(map[[20]byte]*Torrent),
	}
	for _, i := range infos {
		t.Torrents[i.SHA1] = &Torrent{
			Info:  i,
			Peers: make(map[string]*Peer),
		}
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			<-ticker.C
			for _, torrent := range t.Torrents {
				torrent.mu.Lock()
				peers := maps.Clone(torrent.Peers)
				for addr, p := range peers {
					if p.LastEvent == "stopped" || time.Since(p.UpdatedAt) > time.Minute {
						delete(torrent.Peers, addr)
					}
				}
				torrent.mu.Unlock()
			}
		}
	}()
	return t
}

func (t *HTTP) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	r := &trackerapi.Request{}
	fail := func(err error) {
		res := &trackerapi.Response{
			FailureReason: err.Error(),
		}
		data, err := bencode.Marshal(res)
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			slog.Error("ServeHTTP", "err", err)
			return
		}
		rw.WriteHeader(http.StatusBadRequest)
		rw.Write(data)
	}
	err := r.Decode(req.URL.RawQuery)
	if err != nil {
		fail(err)
		return
	}
	torrent, ok := t.Torrents[r.InfoHash]
	if !ok {
		fail(errors.New("unknown info hash"))
		return
	}
	var ip string
	if r.IP == "" {
		ip, _, err = net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
	} else {
		ip = r.IP
	}
	netIP := net.ParseIP(ip)
	if netIP == nil {
		fail(errors.New("bad ip"))
		return
	}
	if r.Port == 0 {
		fail(errors.New("bad port"))
		return
	}
	addr := fmt.Sprintf("%s:%d", netIP.String(), r.Port)
	torrent.mu.Lock()
	defer torrent.mu.Unlock()
	peer, ok := torrent.Peers[addr]
	if !ok {
		peer = &Peer{
			tp: &trackerapi.Peer{
				ID:   string(r.PeerID[:]),
				IP:   ip,
				Port: r.Port,
			},
		}
		torrent.Peers[addr] = peer
	}
	peer.UpdatedAt = time.Now()
	peer.LastEvent = r.Event
	peers := &trackerapi.Peers{Compact: r.Compact}
	for _, p := range torrent.Peers {
		if p.LastEvent == "stopped" || time.Since(p.UpdatedAt) > time.Minute {
			continue
		}
		paddr := fmt.Sprintf("%s:%d", p.tp.IP, p.tp.Port)
		if paddr != addr {
			peers.List = append(peers.List, p.tp)
		}
	}
	res := &trackerapi.Response{
		Interval: 30,
		Peers:    peers,
	}
	data, err := bencode.Marshal(res)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		slog.Error("ServeHTTP", "err", err)
		return
	}
	rw.Write(data)
}
