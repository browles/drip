package metainfo

import (
	"crypto/sha1"
	"fmt"
	"strings"
	"time"

	"github.com/browles/drip/bencode"
)

type Metainfo struct {
	Info     *Info  `bencode:"info"`
	Announce string `bencode:"announce"`
	// http://bittorrent.org/beps/bep_0012.html
	AnnounceList [][]string `bencode:"announce-list,omitempty"`
	CreationDate *Time      `bencode:"creation date,omitempty"`
	Comment      string     `bencode:"comment,omitempty"`
	CreatedBy    string     `bencode:"created by,omitempty"`
	Encoding     string     `bencode:"encoding,omitempty"`
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
	// http://bittorrent.org/beps/bep_0027.html
	Private int `bencode:"private,omitempty"`
	// Advisory filename or directory, depending on the mode
	Name string `bencode:"name"`
	// Single file mode
	Length int64  `bencode:"length,omitempty"`
	MD5Sum string `bencode:"md5sum,omitempty"`
	// Multi file mode
	Files []*File `bencode:"files,omitempty"`
}

func (i *Info) GetLength() int64 {
	if len(i.Files) > 0 {
		var sum int64
		for _, f := range i.Files {
			sum += f.Length
		}
		return sum
	}
	return i.Length
}

func (i *Info) GetPieceLength(index int) int {
	if index == len(i.Pieces)-1 {
		return int(i.GetLength() - int64(index)*int64(i.PieceLength))
	}
	return i.PieceLength
}

type info Info

func (i *Info) UnmarshalBencoding(data []byte) error {
	cpy := &info{}
	if err := bencode.Unmarshal(data, cpy); err != nil {
		return err
	}
	*i = Info(*cpy)
	i.SHA1 = sha1.Sum(data)
	return nil
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
	Length int64    `bencode:"length"`
	MD5Sum string   `bencode:"md5sum,omitempty"`
	Path   []string `bencode:"path"`
}
