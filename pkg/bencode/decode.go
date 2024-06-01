package bencode

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"unicode"
)

func Unmarshal(b []byte, v any) error {
	d := NewDecoder(bytes.NewReader(b))
	return d.Decode(v)
}

type Decoder struct {
	*bufio.Reader
	offset int
}

func NewDecoder(r io.Reader) *Decoder {
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
	t := v.Type()
	if t.Implements(unmarshalerType) {
		return d.decodeUnmarshaler(v)
	}
	if t == byteSliceType {
		return d.decodeString(v)
	}
	switch v.Kind() {
	case reflect.String:
		return d.decodeString(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return d.decodeInt(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return d.decodeUint(v)
	case reflect.Slice, reflect.Array:
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
		return &UnsupportedTypeError{t}
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
		if unicode.IsDigit(rune(b)) {
			return d.consumeString(buf)
		}
		return d.newSyntaxError("unexpected prefix: %c", b)
	}
}

func (d *Decoder) consumeString(buf *bytes.Buffer) error {
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
	if buf != nil {
		buf.Write([]byte(lengthStr)) // lengthStr is already terminated with :
		buf.Write(bytes)
	}
	return nil
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
	_, err = strconv.ParseInt(intStr[:len(intStr)-1], 10, 64)
	if err != nil {
		return d.newSyntaxError("ParseInt: %w", err)
	}
	d.offset += len(intStr)
	if buf != nil {
		buf.WriteByte('i')
		buf.Write([]byte(intStr)) // intStr is already terminated with e
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
		if err := d.consumeString(buf); err != nil {
			return err
		}
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
	if v.Type() == byteSliceType {
		v.SetBytes(bytes)
	} else {
		v.SetString(string(bytes))
	}
	return nil
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
	i, err := strconv.ParseInt(intStr[:len(intStr)-1], 10, 64)
	if err != nil {
		return d.newSyntaxError("ParseInt: %w", err)
	}
	d.offset += len(intStr)
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
	i, err := strconv.ParseUint(intStr[:len(intStr)-1], 10, 64)
	if err != nil {
		return d.newSyntaxError("ParseUint: %w", err)
	}
	d.offset += len(intStr)
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
			// Pass the pointer to check for a unmarshaler with a pointer receiver.
			if err := d.decode(v.Index(i).Addr()); err != nil {
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
	if kt.Kind() != reflect.String {
		return &UnsupportedTypeError{t}
	}
	if v.IsNil() {
		v.Set(reflect.MakeMap(t))
	}
	et := t.Elem()
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
	byteSliceType    = reflect.TypeFor[[]byte]()
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
		// Pass the pointer to check for a unmarshaler with a pointer receiver.
		if err := d.decode(elem.Addr()); err != nil {
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
		if unicode.IsDigit(rune(b)) {
			iv = reflect.New(stringType)
		} else {
			return d.newSyntaxError("unexpected prefix: %c", b)
		}
	}
	if err := d.decode(iv); err != nil {
		return err
	}
	v.Set(iv.Elem())
	return nil
}
