package magnet

import (
	"net/url"
	"strconv"
)

type Magnet struct {
	ExactTopic string
	Name       string
	Length     int
	Trackers   []*url.URL
	WebSeed    string
	Peers      []string
}

func ParseMagnetURL(link string) (*Magnet, error) {
	url, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	query := url.Query()
	ma := &Magnet{
		ExactTopic: query.Get("xt"),
		Name:       query.Get("dn"),
		WebSeed:    query.Get("ws"),
		Peers:      query["x.pe"],
	}
	xl := query.Get("xl")
	if xl != "" {
		ma.Length, err = strconv.Atoi(xl)
		if err != nil {
			return nil, err
		}
	}
	for _, tr := range query["tr"] {
		tracker, err := url.Parse(tr)
		if err != nil {
			return nil, err
		}
		ma.Trackers = append(ma.Trackers, tracker)
	}
	return ma, nil
}
