package p2p

import (
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"slices"

	"github.com/browles/drip/api/metainfo"
	trackerapi "github.com/browles/drip/api/tracker"
	"github.com/browles/drip/bencode"
)

// https://www.bittorrent.org/beps/bep_0012.html
type Multitracker struct {
	TrackerTiers [][]*Tracker
}

func NewMultitracker(mi *metainfo.Metainfo, s *Server) *Multitracker {
	var announceList [][]string
	if len(mi.AnnounceList) > 0 {
		announceList = mi.AnnounceList
	} else {
		announceList = [][]string{{mi.Announce}}
	}
	mt := &Multitracker{}
	for _, list := range announceList {
		cpy := slices.Clone(list)
		rand.Shuffle(len(cpy), func(i, j int) {
			cpy[i], cpy[j] = cpy[j], cpy[i]
		})
		var tier []*Tracker
		for _, url := range cpy {
			tier = append(tier, &Tracker{URL: url, server: s})
		}
		mt.TrackerTiers = append(mt.TrackerTiers, tier)
	}
	return mt
}

func (mt *Multitracker) Start() (*trackerapi.Response, error) {
	return mt.get(STARTED)
}

func (mt *Multitracker) Complete() (*trackerapi.Response, error) {
	return mt.get(COMPLETED)
}

func (mt *Multitracker) Stop() (*trackerapi.Response, error) {
	return mt.get(STOPPED)
}

func (mt *Multitracker) Announce() (*trackerapi.Response, error) {
	return mt.get(ANNOUNCE)
}

func (mt *Multitracker) get(event eventName) (res *trackerapi.Response, err error) {
	for _, tier := range mt.TrackerTiers {
		for j, tracker := range tier {
			res, err = tracker.get(event)
			if err == nil {
				copy(tier[1:j+1], tier[0:j])
				tier[0] = tracker
				return
			}
			slog.Error("get", "err", err)
		}
	}
	return
}

type Tracker struct {
	URL    string
	server *Server
}

type eventName string

const (
	STARTED   eventName = "started"
	COMPLETED eventName = "completed"
	STOPPED   eventName = "stopped"
	ANNOUNCE  eventName = ""
)

func (t *Tracker) Start() (*trackerapi.Response, error) {
	return t.get(STARTED)
}

func (t *Tracker) Complete() (*trackerapi.Response, error) {
	return t.get(COMPLETED)
}

func (t *Tracker) Stop() (*trackerapi.Response, error) {
	return t.get(STOPPED)
}

func (t *Tracker) Announce() (*trackerapi.Response, error) {
	return t.get(ANNOUNCE)
}

func (mt *Tracker) get(event eventName) (*trackerapi.Response, error) {
	req := &trackerapi.Request{
		InfoHash:   mt.server.Info.SHA1,
		PeerID:     mt.server.PeerID,
		Port:       mt.server.Port,
		Uploaded:   mt.server.Uploaded(),
		Downloaded: mt.server.Downloaded(),
		Left:       mt.server.Info.Length - mt.server.Storage.Downloaded(),
		Event:      string(event),
		Compact:    true,
	}
	url, err := url.Parse(mt.URL)
	if err != nil {
		return nil, err
	}
	url.RawQuery = req.Encode()
	httpRes, err := http.Get(url.String())
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(httpRes.Body)
	if err != nil {
		return nil, err
	}
	res := &trackerapi.Response{}
	if err := bencode.Unmarshal(data, res); err != nil {
		return nil, err
	}
	if res.FailureReason != "" {
		return nil, errors.New("tracker: request failed: " + res.FailureReason)
	}
	return res, nil
}
