package magnet

import (
	"net/url"
	"strconv"
)

type Magnet struct {
	ExactTopic string
	Name       string
	Length     int
	Trackers   []string
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
		Trackers:   query["tr"],
		WebSeed:    query.Get("ws"),
		Peers:      query["x.pe"],
	}
	xl := query.Get("xl")
	if xl != "" {
		if ma.Length, err = strconv.Atoi(xl); err != nil {
			return nil, err
		}
	}
	return ma, nil
}
