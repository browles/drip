package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/browles/gotorrent/pkg/bencode"
)

type Metainfo struct {
	Info         *InfoDictionary `bencode:"info"`
	Announce     string          `bencode:"announce"`
	AnnounceList [][]string      `bencode:"announce-list"`
	CreationDate Time            `bencode:"creation date"`
	Comment      string          `bencode:"comment"`
	CreatedBy    string          `bencode:"created by"`
	Encoding     string          `bencode:"encoding"`
}

type Time struct {
	time.Time
}

func (t *Time) MarshalBencoding() ([]byte, error) {
	return bencode.Marshal(t.Unix())
}

func (t *Time) UnmarshalBencoding(data []byte) error {
	var i int64
	if err := bencode.Unmarshal(data, &i); err != nil {
		return err
	}
	t.Time = time.Unix(i, 0)
	return nil
}

type InfoDictionary struct {
	PieceLength int       `bencode:"piece length"`
	Pieces      SHA1Slice `bencode:"pieces"`
	Private     int       `bencode:"private"`
	// Advisory filename or directory, depending on the mode
	Name string `bencode:"name"`
	// Single file mode
	Length int    `bencode:"length"`
	MD5Sum string `bencode:"md5sum"`
	// Multi file mode
	Files []*File `bencode:"files"`
}

type SHA1Slice []string

func (s *SHA1Slice) MarshalBencoding() ([]byte, error) {
	var sb strings.Builder
	for _, hash := range *s {
		sb.Write([]byte(hash))
	}
	return bencode.Marshal(sb.String())
}

func (s *SHA1Slice) UnmarshalBencoding(data []byte) error {
	var concat string
	if err := bencode.Unmarshal(data, &concat); err != nil {
		return err
	}
	if len(concat)%20 != 0 {
		return fmt.Errorf("cannot unmarshal pieces string, len() not multiple of 20: %d", len(concat))
	}
	var res []string
	for i := 0; i+20 <= len(concat); i += 20 {
		res = append(res, concat[i:i+20])
	}
	*s = SHA1Slice(res)
	return nil
}

type Files struct {
	Name string `bencode:"name"`
}

type File struct {
	Length int      `bencode:"length"`
	MD5Sum string   `bencode:"md5sum"`
	Path   []string `bencode:"path"`
}
