package p2p

import (
	"errors"
	"io"
	"net/http"
	"net/url"

	trackerapi "github.com/browles/drip/api/tracker"
	"github.com/browles/drip/bencode"
)

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
