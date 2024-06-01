package bencode

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
)

func Marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	e := NewEncoder(&b)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

type Encoder struct {
	io.Writer
	ptrDepth int
}

const ptrDepthLimit = 1000

func NewEncoder(w io.Writer) Encoder {
	return Encoder{Writer: w}
}

func (e *Encoder) Encode(v any) error {
	return e.encode(reflect.ValueOf(v))
}

type UnsupportedTypeError struct {
	Type reflect.Type
}

func (e *UnsupportedTypeError) Error() string {
	return "bencode: unsupported type: " + e.Type.String()
}

type Marshaler interface {
	MarshalBencoding() ([]byte, error)
}

func (e *Encoder) writeByte(c byte) error {
	_, err := e.Write([]byte{c})
	return err
}

func (e *Encoder) writeString(s string) (n int, err error) {
	return io.WriteString(e.Writer, s)
}

var marshalerType = reflect.TypeFor[Marshaler]()

func (e *Encoder) encode(v reflect.Value) error {
	if v.Type().Implements(marshalerType) {
		return e.encodeMarshaler(v)
	}
	if v.Type() == byteSliceType {
		return e.encodeBytes(v)
	}
	switch v.Kind() {
	case reflect.String:
		return e.encodeString(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return e.encodeInt(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return e.encodeUint(v)
	case reflect.Slice:
		return e.encodeSlice(v)
	case reflect.Map:
		return e.encodeMap(v)
	case reflect.Struct:
		return e.encodeStruct(v)
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return e.encode(v.Elem())
	default:
		return &UnsupportedTypeError{v.Type()}
	}
}

func (e *Encoder) encodeMarshaler(v reflect.Value) error {
	m, ok := v.Interface().(Marshaler)
	if !ok {
		panic("passed value does not implement Marshaler")
	}
	b, err := m.MarshalBencoding()
	if err != nil {
		return err
	}
	_, err = e.Write(b)
	return err
}

func (e *Encoder) encodeString(v reflect.Value) error {
	s := v.String()
	if _, err := e.writeString(strconv.Itoa(len(s))); err != nil {
		return err
	}
	if err := e.writeByte(':'); err != nil {
		return err
	}
	if _, err := e.writeString(s); err != nil {
		return err
	}
	return nil
}

func (e *Encoder) encodeBytes(v reflect.Value) error {
	b := v.Bytes()
	if _, err := e.writeString(strconv.Itoa(len(b))); err != nil {
		return err
	}
	if err := e.writeByte(':'); err != nil {
		return err
	}
	if _, err := e.Write(b); err != nil {
		return err
	}
	return nil
}

func (e *Encoder) encodeInt(v reflect.Value) error {
	i := v.Int()
	if err := e.writeByte('i'); err != nil {
		return err
	}
	if _, err := e.writeString(strconv.FormatInt(i, 10)); err != nil {
		return err
	}
	if err := e.writeByte('e'); err != nil {
		return err
	}
	return nil
}

func (e *Encoder) encodeUint(v reflect.Value) error {
	i := v.Uint()
	if err := e.writeByte('i'); err != nil {
		return err
	}
	if _, err := e.writeString(strconv.FormatUint(i, 10)); err != nil {
		return err
	}
	if err := e.writeByte('e'); err != nil {
		return err
	}
	return nil
}

func (e *Encoder) encodeSlice(v reflect.Value) error {
	if v.IsNil() {
		return nil
	}
	if e.ptrDepth++; e.ptrDepth > ptrDepthLimit {
		return errors.New("bencode: maximum pointer depth exceeded")
	}
	if err := e.writeByte('l'); err != nil {
		return err
	}
	for i := range v.Len() {
		if err := e.encode(v.Index(i)); err != nil {
			return err
		}
	}
	if err := e.writeByte('e'); err != nil {
		return err
	}
	e.ptrDepth--
	return nil
}

func (e *Encoder) encodeMap(v reflect.Value) error {
	if v.IsNil() {
		return nil
	}
	if v.Type().Key().Kind() != reflect.String {
		return &UnsupportedTypeError{v.Type()}
	}
	if e.ptrDepth++; e.ptrDepth > ptrDepthLimit {
		return errors.New("bencode: maximum pointer depth exceeded")
	}
	if err := e.writeByte('d'); err != nil {
		return err
	}
	keys := v.MapKeys()
	slices.SortFunc(keys, func(a, b reflect.Value) int {
		return strings.Compare(a.String(), b.String())
	})
	for _, key := range keys {
		if err := e.encodeString(key); err != nil {
			return err
		}
		if err := e.encode(v.MapIndex(key)); err != nil {
			return err
		}
	}
	if err := e.writeByte('e'); err != nil {
		return err
	}
	e.ptrDepth--
	return nil
}

type fieldInfo struct {
	fields     []*field
	keyToField map[string]*field
}

type field struct {
	ignored bool
	index   []int
	name    string
	tag     string
	key     string
	typ     reflect.Type
	field   reflect.StructField
}

var (
	fieldCacheMu sync.Mutex
	fieldCache   = make(map[reflect.Type]*fieldInfo)
)

func getFieldsForStruct(v reflect.Value) *fieldInfo {
	t := v.Type()
	fieldCacheMu.Lock()
	if fi, ok := fieldCache[t]; ok {
		fieldCacheMu.Unlock()
		return fi
	}
	fieldCacheMu.Unlock()
	keyToField := make(map[string]*field)
	visited := make(map[reflect.Type]struct{})
	curr, next := []*field{}, []*field{{typ: t}}
	for len(next) > 0 {
		curr, next = next, curr[:0]
		for _, structField := range curr {
			t := structField.typ
			if _, ok := visited[t]; ok {
				continue
			}
			visited[t] = struct{}{}
			for j := range t.NumField() {
				sf := t.Field(j)
				ft := sf.Type
				if sf.Anonymous {
					if ft.Kind() == reflect.Pointer {
						ft = ft.Elem()
					}
					// If anonymous struct field is not exported, we still need to account
					// for exported embedded fields.
					if !sf.IsExported() && ft.Kind() != reflect.Struct {
						continue
					}
				} else if !sf.IsExported() {
					continue
				}
				tag := sf.Tag.Get("bencode")
				if tag == "-" {
					continue
				}
				tag = strings.ToLower(tag)
				name := strings.ToLower(sf.Name)
				index := make([]int, len(structField.index)+1)
				copy(index, structField.index)
				index[len(index)-1] = j
				key := tag
				if key == "" {
					key = name
				}
				f := &field{
					index: index,
					name:  name,
					tag:   tag,
					key:   key,
					typ:   sf.Type,
					field: sf,
				}
				if tag == "" && sf.Anonymous && ft.Kind() == reflect.Struct {
					next = append(next, f)
				} else {
					if ef, ok := keyToField[key]; ok {
						if len(ef.index) < len(f.index) || ef.ignored {
							// Existing field has lower depth, or field is already ignored
							// from a previous conflict, do not replace.
							continue
						}
						if (f.tag != "" && f.tag == ef.tag) ||
							(ef.tag == "" && f.name == ef.name) {
							// Existing field has the same tag, or neither fields have tags
							// but share a name, ignore.
							keyToField[key].ignored = true
						} else if f.tag != "" && ef.tag == "" {
							// New field has a tag while existing does not, replace.
							keyToField[key] = f
						}

					} else {
						keyToField[key] = f
					}
				}
			}
		}
	}
	var fields []*field
	for _, f := range keyToField {
		if f.ignored {
			continue
		}
		fields = append(fields, f)
	}
	slices.SortFunc(fields, func(a, b *field) int {
		return strings.Compare(a.key, b.key)
	})
	fi := &fieldInfo{
		fields:     fields,
		keyToField: keyToField,
	}
	fieldCacheMu.Lock()
	fieldCache[t] = fi
	fieldCacheMu.Unlock()
	return fi
}

func isNil(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Slice, reflect.Map, reflect.Interface, reflect.Pointer:
		return v.IsNil()
	default:
		return false
	}
}

func (e *Encoder) encodeStruct(v reflect.Value) error {
	if e.ptrDepth++; e.ptrDepth > ptrDepthLimit {
		return errors.New("bencode: maximum pointer depth exceeded")
	}
	if err := e.writeByte('d'); err != nil {
		return err
	}
	for _, f := range getFieldsForStruct(v).fields {
		fv := v.FieldByIndex(f.index)
		if isNil(fv) {
			continue
		}
		if fv.Kind() == reflect.Pointer {
			fv = fv.Elem()
		}
		if err := e.encodeString(reflect.ValueOf(f.key)); err != nil {
			return err
		}
		if err := e.encode(fv); err != nil {
			return err
		}
	}
	if err := e.writeByte('e'); err != nil {
		return err
	}
	e.ptrDepth--
	return nil
}
