package bencode

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode"
)

type runeReader interface {
	io.Reader
	io.RuneScanner
}

type (
	Bencoder interface {
		Encode() []byte
	}
	ByteString string
	Integer    int
	List       []Bencoder
	Dictionary map[ByteString]Bencoder
)

func (b ByteString) Encode() []byte {
	length := len(b)
	lenStr := strconv.Itoa(length)
	res := make([]byte, len(lenStr)+length+1)
	i := copy(res, lenStr)
	i += copy(res[i:], ":")
	copy(res[i:], b)
	return res
}

func (i Integer) Encode() []byte {
	iStr := strconv.Itoa(int(i))
	res := make([]byte, len(iStr)+2)
	j := copy(res, "i")
	j += copy(res[j:], iStr)
	copy(res[j:], "e")
	return res
}

func (l List) Encode() []byte {
	var res bytes.Buffer
	res.Write([]byte("l"))
	for _, item := range l {
		res.Write(item.Encode())
	}
	res.Write([]byte("e"))
	return res.Bytes()
}

func (d Dictionary) Encode() []byte {
	var res bytes.Buffer
	res.Write([]byte("d"))
	for key, value := range d {
		res.Write(key.Encode())
		res.Write(value.Encode())
	}
	res.Write([]byte("e"))
	return res.Bytes()
}

func decodeByteString(r runeReader) (ByteString, error) {
	var lengthRunes []rune
	for {
		c, _, err := r.ReadRune()
		if err != nil {
			return "", err
		}
		if c == ':' {
			break
		}
		if !unicode.IsDigit(c) {
			return "", fmt.Errorf("unexpected bytestring prefix: %c", c)
		}
		lengthRunes = append(lengthRunes, c)
	}
	length, err := strconv.Atoi(string(lengthRunes))
	if err != nil {
		return "", err
	}
	bytes := make([]byte, length)
	n, err := r.Read(bytes)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if n < length {
		return "", fmt.Errorf("unexpected bytes: %d < %d", n, length)
	}
	return ByteString(bytes), nil
}

func decodeInteger(r runeReader) (Integer, error) {
	c, _, err := r.ReadRune()
	if err != nil {
		return 0, err
	}
	if c != 'i' {
		return 0, fmt.Errorf("unexpected integer prefix: %c", c)
	}
	integerRunes := make([]rune, 0)
	for {
		c, _, err := r.ReadRune()
		if err != nil {
			return 0, err
		}
		if c == 'e' {
			break
		}
		if !unicode.IsDigit(c) && c != '-' {
			return 0, fmt.Errorf("unexpected digit: %c", c)
		}
		integerRunes = append(integerRunes, c)
	}
	i, err := strconv.Atoi(string(integerRunes))
	if err != nil {
		return 0, err
	}
	return Integer(i), nil
}

func decodeList(r runeReader) (List, error) {
	c, _, err := r.ReadRune()
	if err != nil {
		return nil, err
	}
	if c != 'l' {
		return nil, fmt.Errorf("unexpected list prefix: %c", c)
	}
	var list List
	for {
		c, _, err := r.ReadRune()
		if err != nil {
			return nil, err
		}
		if c == 'e' {
			break
		}
		if err := r.UnreadRune(); err != nil {
			return nil, err
		}
		decoded, err := decode(r)
		if err != nil {
			return nil, err
		}
		list = append(list, decoded)
	}
	return list, nil
}

func decodeDictionary(r runeReader) (Dictionary, error) {
	c, _, err := r.ReadRune()
	if err != nil {
		return nil, err
	}
	if c != 'd' {
		return nil, fmt.Errorf("unexpected dictionary prefix: %c", c)
	}
	dict := make(Dictionary)
	for {
		c, _, err := r.ReadRune()
		if err != nil {
			return nil, err
		}
		if c == 'e' {
			break
		}
		if err := r.UnreadRune(); err != nil {
			return nil, err
		}
		key, err := decodeByteString(r)
		if err != nil {
			return nil, err
		}
		value, err := decode(r)
		if err != nil {
			return nil, err
		}
		dict[key] = value
	}
	return dict, nil
}

func decode(r runeReader) (Bencoder, error) {
	c, _, err := r.ReadRune()
	if err != nil {
		return nil, err
	}
	if err = r.UnreadRune(); err != nil {
		return nil, err
	}
	switch c {
	case 'i':
		return decodeInteger(r)
	case 'l':
		return decodeList(r)
	case 'd':
		return decodeDictionary(r)
	default:
		return decodeByteString(r)
	}
}

func DecodeByteString(b []byte) (ByteString, error) {
	return decodeByteString(bytes.NewReader(b))
}

func DecodeInteger(b []byte) (Integer, error) {
	return decodeInteger(bytes.NewReader(b))
}

func DecodeList(b []byte) (List, error) {
	return decodeList(bytes.NewReader(b))
}

func DecodeDictionary(b []byte) (Dictionary, error) {
	return decodeDictionary(bytes.NewReader(b))
}

func Decode(b []byte) (Bencoder, error) {
	return decode(bytes.NewReader(b))
}
