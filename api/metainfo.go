package api

import (
	"crypto/sha1"
	"fmt"
	"strings"
	"time"

	"github.com/browles/drip/bencode"
)

type Metainfo struct {
	InfoBytes    bencode.RawMessage `bencode:"info"`
	Info         *Info              `bencode:"-"`
	Announce     string             `bencode:"announce"`
	AnnounceList [][]string         `bencode:"announce-list,omitempty"`
	CreationDate *Time              `bencode:"creation date,omitempty"`
	Comment      string             `bencode:"comment,omitempty"`
	CreatedBy    string             `bencode:"created by,omitempty"`
	Encoding     string             `bencode:"encoding,omitempty"`
}

func (mu *Metainfo) Marshal() ([]byte, error) {
	if err := mu.MarshalInfo(); err != nil {
		return nil, err
	}
	return bencode.Marshal(mu)
}

func (mu *Metainfo) MarshalInfo() error {
	if len(mu.InfoBytes) > 0 {
		return nil
	}
	var err error
	mu.InfoBytes, err = bencode.Marshal(mu.Info)
	return err
}

func (mu *Metainfo) Unmarshal(data []byte) error {
	if err := bencode.Unmarshal(data, mu); err != nil {
		return err
	}
	return mu.UnmarshalInfo()
}

func (mu *Metainfo) UnmarshalInfo() error {
	mu.Info = &Info{}
	err := bencode.Unmarshal(mu.InfoBytes, mu.Info)
	if err != nil {
		return err
	}
	mu.Info.SHA1 = sha1.Sum(mu.InfoBytes)
	return nil
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

type Info struct {
	SHA1        [20]byte `bencode:"-"`
	PieceLength int      `bencode:"piece length"`
	Pieces      Pieces   `bencode:"pieces"`
	Private     int      `bencode:"private,omitempty"`
	// Advisory filename or directory, depending on the mode
	Name string `bencode:"name"`
	// Single file mode
	Length int    `bencode:"length,omitempty"`
	MD5Sum string `bencode:"md5sum,omitempty"`
	// Multi file mode
	Files []*File `bencode:"files,omitempty"`
}

type Pieces [][20]byte

func (p *Pieces) MarshalBencoding() ([]byte, error) {
	var sb strings.Builder
	for _, hash := range *p {
		sb.Write(hash[:])
	}
	return bencode.Marshal(sb.String())
}

func (p *Pieces) UnmarshalBencoding(data []byte) error {
	var concat []byte
	if err := bencode.Unmarshal(data, &concat); err != nil {
		return err
	}
	if len(concat)%20 != 0 {
		return fmt.Errorf("cannot unmarshal pieces string, len() not multiple of 20: %d", len(concat))
	}
	var res [][20]byte
	for i := 0; i < len(concat); i += 20 {
		var a [20]byte
		copy(a[:], concat[i:i+20])
		res = append(res, a)
	}
	*p = Pieces(res)
	return nil
}

type File struct {
	Length int      `bencode:"length"`
	MD5Sum string   `bencode:"md5sum,omitempty"`
	Path   []string `bencode:"path"`
}
