package bencode

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
)

func Unmarshal(b []byte, v any) error {
	r := bytes.NewReader(b)
	d := NewDecoder(bufio.NewReaderSize(r, 32))
	return d.Decode(v)
}

type Decoder struct {
	*bufio.Reader
	offset int
}

func NewDecoder(r io.Reader) *Decoder {
	if br, ok := r.(*bufio.Reader); ok {
		return &Decoder{Reader: br}
	}
	return &Decoder{Reader: bufio.NewReader(r)}
}

func (d *Decoder) Decode(v any) error {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Interface {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("bencode: must decode into non-nil pointer")
	}
	return d.decode(rv)
}

type Unmarshaler interface {
	UnmarshalBencoding([]byte) error
}

func (d *Decoder) newSyntaxError(t string, args ...any) error {
	return fmt.Errorf(fmt.Sprintf("bencode: syntax error starting at offset %d: %s", d.offset, t), args...)
}

var unmarshalerType = reflect.TypeFor[Unmarshaler]()

func (d *Decoder) decode(v reflect.Value) error {
	if v.Kind() != reflect.Pointer && reflect.PointerTo(v.Type()).Implements(unmarshalerType) && v.CanAddr() {
		return d.decodeUnmarshaler(v.Addr())
	}
	if v.Type().Implements(unmarshalerType) {
		return d.decodeUnmarshaler(v)
	}
	switch v.Kind() {
	case reflect.String:
		return d.decodeString(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return d.decodeInt(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return d.decodeUint(v)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return d.decodeString(v)
		}
		return d.decodeSlice(v)
	case reflect.Array:
		return d.decodeSlice(v)
	case reflect.Map:
		return d.decodeMap(v)
	case reflect.Struct:
		return d.decodeStruct(v)
	case reflect.Interface:
		return d.decodeInterface(v)
	case reflect.Pointer:
		return d.decode(v.Elem())
	default:
		return &UnsupportedTypeError{v.Type()}
	}
}

func (d *Decoder) decodeUnmarshaler(v reflect.Value) error {
	m, ok := v.Interface().(Unmarshaler)
	if !ok {
		panic("passed value does not implement Unmarshaler")
	}
	var b bytes.Buffer
	if err := d.consume(&b); err != nil {
		return err
	}
	return m.UnmarshalBencoding(b.Bytes())
}

func isDigit(b byte) bool {
	return '0' <= b && b <= '9'
}

func (d *Decoder) consume(buf *bytes.Buffer) error {
	b, err := d.ReadByte()
	if err != nil {
		return err
	}
	if err = d.UnreadByte(); err != nil {
		return err
	}
	switch b {
	case 'i':
		return d.consumeInt(buf)
	case 'l':
		return d.consumeList(buf)
	case 'd':
		return d.consumeDictionary(buf)
	default:
		if isDigit(b) {
			_, err := d.consumeString(buf)
			return err
		}
		return d.newSyntaxError("unexpected prefix: %c", b)
	}
}

func (d *Decoder) consumeString(buf *bytes.Buffer) (string, error) {
	lengthStr, err := d.ReadString(':')
	if err != nil {
		return "", d.newSyntaxError("unterminated string prefix: %w", err)
	}
	lengthStr = lengthStr[:len(lengthStr)-1]
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return "", d.newSyntaxError("Atoi: %w", err)
	}
	d.offset += len(lengthStr) + 1
	bytes := make([]byte, length)
	n, err := io.ReadFull(d, bytes)
	if err != nil {
		return "", d.newSyntaxError("unexpected string eof: %d < %d: %w", n, length, err)
	}
	d.offset += n
	if buf != nil {
		buf.Write([]byte(lengthStr))
		buf.WriteByte(':')
		buf.Write(bytes)
	}
	return string(bytes), nil
}

func (d *Decoder) consumeInt(buf *bytes.Buffer) error {
	b, err := d.ReadByte()
	if err != nil {
		return err
	}
	if b != 'i' {
		return d.newSyntaxError("unexpected integer prefix: %c", b)
	}
	d.offset += 1
	intStr, err := d.ReadString('e')
	if err != nil {
		return d.newSyntaxError("unterminated int: %w", err)
	}
	intStr = intStr[:len(intStr)-1]
	if leadingZero(intStr) {
		return d.newSyntaxError("invalid int: %s", intStr)
	}
	_, err = strconv.ParseInt(intStr, 10, 64)
	if err != nil {
		return d.newSyntaxError("ParseInt: %w", err)
	}
	d.offset += len(intStr) + 1
	if buf != nil {
		buf.WriteByte('i')
		buf.Write([]byte(intStr))
		buf.WriteByte('e')
	}
	return nil
}

func (d *Decoder) consumeList(buf *bytes.Buffer) error {
	b, err := d.ReadByte()
	if err != nil {
		return err
	}
	if b != 'l' {
		return d.newSyntaxError("unexpected list prefix: %c", b)
	}
	d.offset += 1
	if buf != nil {
		buf.WriteByte('l')
	}
	for {
		c, err := d.ReadByte()
		if err != nil {
			return err
		}
		if c == 'e' {
			break
		}
		if err := d.UnreadByte(); err != nil {
			return err
		}
		err = d.consume(buf)
		if err != nil {
			return err
		}
	}
	d.offset += 1
	if buf != nil {
		buf.WriteByte('e')
	}
	return nil
}

func (d *Decoder) consumeDictionary(buf *bytes.Buffer) error {
	c, err := d.ReadByte()
	if err != nil {
		return err
	}
	if c != 'd' {
		return d.newSyntaxError("unexpected dictionary prefix: %c", c)
	}
	d.offset += 1
	if buf != nil {
		buf.WriteByte('d')
	}
	lastKey := ""
	for {
		c, err := d.ReadByte()
		if err != nil {
			return err
		}
		if c == 'e' {
			break
		}
		if err := d.UnreadByte(); err != nil {
			return err
		}
		key, err := d.consumeString(buf)
		if err != nil {
			return err
		}
		if lastKey >= key {
			return fmt.Errorf("dictionary keys not sorted")
		}
		lastKey = key
		if err := d.consume(buf); err != nil {
			return err
		}
	}
	d.offset += 1
	if buf != nil {
		buf.WriteByte('e')
	}
	return nil
}

func (d *Decoder) decodeString(v reflect.Value) error {
	lengthStr, err := d.ReadString(':')
	if err != nil {
		return d.newSyntaxError("unterminated string prefix: %w", err)
	}
	length, err := strconv.Atoi(lengthStr[:len(lengthStr)-1])
	if err != nil {
		return d.newSyntaxError("Atoi: %w", err)
	}
	d.offset += len(lengthStr)
	bytes := make([]byte, length)
	n, err := io.ReadFull(d, bytes)
	if err != nil {
		return d.newSyntaxError("unexpected string eof: %d < %d: %w", n, length, err)
	}
	d.offset += n
	if v.Kind() == reflect.Slice {
		v.SetBytes(bytes)
	} else {
		v.SetString(string(bytes))
	}
	return nil
}

func leadingZero(intStr string) bool {
	if len(intStr) == 0 {
		return false
	}
	if intStr[0] == '0' {
		return len(intStr) > 1
	}
	if intStr[0] == '-' && len(intStr) >= 2 {
		return intStr[1] == '0'
	}
	return false
}

func (d *Decoder) decodeInt(v reflect.Value) error {
	b, err := d.ReadByte()
	if err != nil {
		return err
	}
	if b != 'i' {
		return d.newSyntaxError("unexpected integer prefix: %c", b)
	}
	d.offset += 1
	intStr, err := d.ReadString('e')
	if err != nil {
		return d.newSyntaxError("unterminated int: %w", err)
	}
	intStr = intStr[:len(intStr)-1]
	if leadingZero(intStr) {
		return d.newSyntaxError("invalid int: %s", intStr)
	}
	i, err := strconv.ParseInt(intStr, 10, 64)
	if err != nil {
		return d.newSyntaxError("ParseInt: %w", err)
	}
	d.offset += len(intStr) + 1
	v.SetInt(i)
	return nil
}

func (d *Decoder) decodeUint(v reflect.Value) error {
	b, err := d.ReadByte()
	if err != nil {
		return err
	}
	if b != 'i' {
		return d.newSyntaxError("unexpected integer prefix: %c", b)
	}
	d.offset += 1
	intStr, err := d.ReadString('e')
	if err != nil {
		return d.newSyntaxError("unterminated int: %w", err)
	}
	intStr = intStr[:len(intStr)-1]
	if leadingZero(intStr) {
		return d.newSyntaxError("invalid int: %s", intStr)
	}
	i, err := strconv.ParseUint(intStr, 10, 64)
	if err != nil {
		return d.newSyntaxError("ParseUint: %w", err)
	}
	d.offset += len(intStr) + 1
	v.SetUint(i)
	return nil
}

func (d *Decoder) decodeSlice(v reflect.Value) error {
	b, err := d.ReadByte()
	if err != nil {
		return err
	}
	if b != 'l' {
		return d.newSyntaxError("unexpected list prefix: %c", b)
	}
	d.offset += 1
	if v.Kind() == reflect.Slice && v.IsNil() {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
	}
	i := 0
	for {
		c, err := d.ReadByte()
		if err != nil {
			return err
		}
		if c == 'e' {
			break
		}
		if err := d.UnreadByte(); err != nil {
			return err
		}
		if v.Kind() == reflect.Slice {
			if i >= v.Cap() {
				if v.Len() > 0 {
					v.Grow(v.Len())
				} else {
					v.Grow(1)
				}
			}
			if i >= v.Len() {
				v.SetLen(i + 1)
			}
		}
		if i < v.Len() {
			// Check length for arrays. This check is guaranteed for slices.
			if err := d.decode(v.Index(i)); err != nil {
				return err
			}
		} else {
			// Toss the remainder.
			if err := d.consume(nil); err != nil {
				return err
			}
		}
		i++
	}
	d.offset += 1
	return nil
}

func (d *Decoder) decodeMap(v reflect.Value) error {
	c, err := d.ReadByte()
	if err != nil {
		return err
	}
	if c != 'd' {
		return d.newSyntaxError("unexpected dictionary prefix: %c", c)
	}
	d.offset += 1
	t := v.Type()
	kt := t.Key()
	et := t.Elem()
	if kt.Kind() != reflect.String {
		return &UnsupportedTypeError{t}
	}
	if v.IsNil() {
		v.Set(reflect.MakeMap(t))
	}
	lastKey := ""
	for {
		c, err := d.ReadByte()
		if err != nil {
			return err
		}
		if c == 'e' {
			break
		}
		if err := d.UnreadByte(); err != nil {
			return err
		}
		key := reflect.New(kt).Elem()
		if err := d.decodeString(key); err != nil {
			return err
		}
		if lastKey >= key.String() {
			return fmt.Errorf("dictionary keys not sorted")
		}
		lastKey = key.String()
		elem := reflect.New(et)
		// Pass the pointer to check for a unmarshaler with a pointer receiver.
		if err := d.decode(elem); err != nil {
			return err
		}
		v.SetMapIndex(key, elem.Elem())
	}
	d.offset += 1
	return nil
}

var (
	intType          = reflect.TypeFor[int]()
	stringType       = reflect.TypeFor[string]()
	anySliceType     = reflect.TypeFor[[]any]()
	mapStringAnyType = reflect.TypeFor[map[string]any]()
)

func (d *Decoder) decodeStruct(v reflect.Value) error {
	c, err := d.ReadByte()
	if err != nil {
		return err
	}
	if c != 'd' {
		return d.newSyntaxError("unexpected dictionary prefix: %c", c)
	}
	d.offset += 1
	keyToField := getFieldsForStruct(v).keyToField
	lastKey := ""
	for {
		c, err := d.ReadByte()
		if err != nil {
			return err
		}
		if c == 'e' {
			break
		}
		if err := d.UnreadByte(); err != nil {
			return err
		}
		key := reflect.New(stringType).Elem()
		if err := d.decodeString(key); err != nil {
			return err
		}
		if lastKey >= key.String() {
			return fmt.Errorf("dictionary keys not sorted")
		}
		lastKey = key.String()
		field, ok := keyToField[key.String()]
		if !ok {
			if err := d.consume(nil); err != nil {
				return err
			}
			continue
		}
		elem := v
		for _, i := range field.index {
			elem = elem.Field(i)
			if elem.Kind() == reflect.Pointer {
				if elem.IsNil() {
					elem.Set(reflect.New(elem.Type().Elem()))
				}
				elem = elem.Elem()
			}
		}
		if err := d.decode(elem); err != nil {
			return err
		}
	}
	d.offset += 1
	return nil
}

func (d *Decoder) decodeInterface(v reflect.Value) error {
	if !v.IsNil() {
		return d.decode(v.Elem())
	} else if v.NumMethod() != 0 {
		// Can only decode into an any.
		return &UnsupportedTypeError{v.Type()}
	}
	b, err := d.ReadByte()
	if err != nil {
		return err
	}
	if err = d.UnreadByte(); err != nil {
		return err
	}
	var iv reflect.Value
	switch b {
	case 'i':
		iv = reflect.New(intType)
	case 'l':
		iv = reflect.New(anySliceType)
	case 'd':
		iv = reflect.New(mapStringAnyType)
	default:
		if isDigit(b) {
			iv = reflect.New(stringType)
		} else {
			return d.newSyntaxError("unexpected prefix: %c", b)
		}
	}
	if err := d.decode(iv.Elem()); err != nil {
		return err
	}
	v.Set(iv.Elem())
	return nil
}
