package bencode

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

type Decoder struct {
	bufio.Reader
}

func (d *Decoder) decodeString() (string, error) {
	lengthStr, err := d.ReadString(':')
	if err != nil {
		return "", err
	}
	length, err := strconv.Atoi(lengthStr[:len(lengthStr)-1])
	if err != nil {
		return "", err
	}
	bytes := make([]byte, length)
	n, err := d.Read(bytes)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if n < length {
		return "", fmt.Errorf("unexpected eof: %d < %d", n, length)
	}
	return string(bytes), nil
}

func (d *Decoder) decodeInt() (int64, error) {
	b, err := d.ReadByte()
	if err != nil {
		return 0, err
	}
	if b != 'i' {
		return 0, fmt.Errorf("unexpected integer prefix: %c", b)
	}
	intStr, err := d.ReadString('e')
	if err != nil {
		return 0, err
	}
	i, err := strconv.ParseInt(intStr[:len(intStr)-1], 10, 64)
	if err != nil {
		return 0, err
	}
	return i, nil
}

func (d *Decoder) decodeList() ([]any, error) {
	b, err := d.ReadByte()
	if err != nil {
		return nil, err
	}
	if b != 'l' {
		return nil, fmt.Errorf("unexpected list prefix: %c", b)
	}
	var list []any
	for {
		c, err := d.ReadByte()
		if err != nil {
			return nil, err
		}
		if c == 'e' {
			break
		}
		if err := d.UnreadByte(); err != nil {
			return nil, err
		}
		decoded, err := d.Decode()
		if err != nil {
			return nil, err
		}
		list = append(list, decoded)
	}
	return list, nil
}

func (d *Decoder) decodeDictionary() (map[string]any, error) {
	c, err := d.ReadByte()
	if err != nil {
		return nil, err
	}
	if c != 'd' {
		return nil, fmt.Errorf("unexpected dictionary prefix: %c", c)
	}
	dict := make(map[string]any)
	for {
		c, err := d.ReadByte()
		if err != nil {
			return nil, err
		}
		if c == 'e' {
			break
		}
		if err := d.UnreadByte(); err != nil {
			return nil, err
		}
		key, err := d.decodeString()
		if err != nil {
			return nil, err
		}
		value, err := d.Decode()
		if err != nil {
			return nil, err
		}
		dict[key] = value
	}
	return dict, nil
}

func (d *Decoder) Decode() (any, error) {
	b, err := d.ReadByte()
	if err != nil {
		return nil, err
	}
	if err = d.UnreadByte(); err != nil {
		return nil, err
	}
	switch b {
	case 'i':
		return d.decodeInt()
	case 'l':
		return d.decodeList()
	case 'd':
		return d.decodeDictionary()
	default:
		return d.decodeString()
	}
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{*bufio.NewReader(r)}
}

func Unmarshal(b []byte) (any, error) {
	d := NewDecoder(bytes.NewReader(b))
	return d.Decode()
}
