package bencode

import (
	"bytes"
	"io"
	"reflect"
	"sort"
	"strconv"
)

type Marshaler interface {
	MarshalBencoding() ([]byte, error)
}

type Encoder struct {
	io.Writer
}

func (e *Encoder) WriteByte(c byte) error {
	_, err := e.Write([]byte{c})
	return err
}

func (e *Encoder) WriteString(s string) (n int, err error) {
	return io.WriteString(e.Writer, s)
}

func (e *Encoder) encodeString(s string) error {
	if _, err := e.WriteString(strconv.Itoa(len(s))); err != nil {
		return err
	}
	if err := e.WriteByte(':'); err != nil {
		return err
	}
	if _, err := e.WriteString(s); err != nil {
		return err
	}
	return nil
}

func (e *Encoder) encodeInt(i int64) error {
	if err := e.WriteByte('i'); err != nil {
		return err
	}
	if _, err := e.WriteString(strconv.FormatInt(i, 10)); err != nil {
		return err
	}
	if err := e.WriteByte('e'); err != nil {
		return err
	}
	return nil
}

func (e *Encoder) encodeUint(i uint64) error {
	if err := e.WriteByte('i'); err != nil {
		return err
	}
	if _, err := e.WriteString(strconv.FormatUint(i, 10)); err != nil {
		return err
	}
	if err := e.WriteByte('e'); err != nil {
		return err
	}
	return nil
}

func (e *Encoder) encodeList(l []any) error {
	if err := e.WriteByte('l'); err != nil {
		return err
	}
	for _, item := range l {
		if err := e.Encode(item); err != nil {
			return err
		}
	}
	if err := e.WriteByte('e'); err != nil {
		return err
	}
	return nil
}

func (e *Encoder) encodeDictionary(d map[string]any) error {
	if err := e.WriteByte('d'); err != nil {
		return err
	}
	keys := make([]string, len(d))
	i := 0
	for key := range d {
		keys[i] = key
		i++
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := e.encodeString(key); err != nil {
			return err
		}
		if err := e.Encode(d[key]); err != nil {
			return err
		}
	}
	if err := e.WriteByte('e'); err != nil {
		return err
	}
	return nil
}

func (e *Encoder) Encode(v any) error {
	switch v := v.(type) {
	case string:
		return e.encodeString(v)
	case int, int8, int16, int32, int64:
		return e.encodeInt(reflect.ValueOf(v).Int())
	case uint, uint8, uint16, uint32, uint64:
		return e.encodeUint(reflect.ValueOf(v).Uint())
	case []any:
		return e.encodeList(v)
	case map[string]any:
		return e.encodeDictionary(v)
	case Marshaler:
		b, err := v.MarshalBencoding()
		if err != nil {
			return err
		}
		_, err = e.Write(b)
		return err
	default:
		panic("unsupported type passed to encode")
	}
}

func NewEncoder(w io.Writer) Encoder {
	return Encoder{w}
}

func Marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	e := NewEncoder(&b)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
