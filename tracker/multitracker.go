package tracker

import (
	"math/rand"

	"github.com/browles/drip/api/metainfo"
	trackerapi "github.com/browles/drip/api/tracker"
	"github.com/browles/drip/p2p"
)

type Multitracker struct {
	TrackerTiers [][]*Tracker
	server       *p2p.Server
}

func New(mi *metainfo.Metainfo, s *p2p.Server) *Multitracker {
	var announceList [][]string
	if len(mi.AnnounceList) > 0 {
		announceList = mi.AnnounceList
	} else {
		announceList = [][]string{{mi.Announce}}
	}
	mt := &Multitracker{server: s}
	for _, list := range announceList {
		rand.Shuffle(len(list), func(i, j int) {
			list[i], list[j] = list[j], list[i]
		})
		var tier []*Tracker
		for _, url := range list {
			tier = append(tier, &Tracker{AnnounceURL: url, server: s})
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
	for _, trackers := range mt.TrackerTiers {
		for j, tracker := range trackers {
			res, err = tracker.get(event)
			if err == nil {
				copy(trackers[1:j+1], trackers[0:j])
				trackers[0] = tracker
				return
			}
		}
	}
	return
}
