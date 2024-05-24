package bencode

import (
	"bytes"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
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
	if v.Kind() != reflect.String {
		panic("passed value is not of kind string")
	}
	s := v.String()
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

func (e *Encoder) encodeInt(v reflect.Value) error {
	i := v.Int()
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

func (e *Encoder) encodeUint(v reflect.Value) error {
	i := v.Uint()
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

func (e *Encoder) encodeSlice(v reflect.Value) error {
	if v.IsNil() {
		return nil
	}
	if err := e.WriteByte('l'); err != nil {
		return err
	}
	for i := range v.Len() {
		if err := e.encode(v.Index(i)); err != nil {
			return err
		}
	}
	if err := e.WriteByte('e'); err != nil {
		return err
	}
	return nil
}

func (e *Encoder) encodeMap(v reflect.Value) error {
	if v.IsNil() {
		return nil
	}
	if err := e.WriteByte('d'); err != nil {
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
	if err := e.WriteByte('e'); err != nil {
		return err
	}
	return nil
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

func getFieldsForStruct(v reflect.Value) []*field {
	fieldMap := make(map[string]*field)
	curr := []*field{}
	next := []*field{{typ: v.Type()}}
	for len(next) > 0 {
		curr, next = next, curr[:0]
		for _, structField := range curr {
			t := structField.typ
			for j := range t.NumField() {
				sf := t.Field(j)
				ft := sf.Type
				if sf.Anonymous {
					if ft.Kind() == reflect.Pointer {
						ft = ft.Elem()
					}
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
					if ef, ok := fieldMap[key]; ok {
						if len(ef.index) < len(f.index) || ef.ignored {
							// Existing field has lower depth, do not replace.
							// Or, field is already ignored from a previous conflict.
							continue
						}
						if (f.tag != "" && f.tag == ef.tag) ||
							(ef.tag == "" && f.name == ef.name) {
							// Existing field has the same tag, ignore.
							// Or, neither fields have tags but share a name, ignore.
							fieldMap[key].ignored = true
						} else if f.tag != "" && ef.tag == "" {
							// New field has a tag while existing does not, replace.
							fieldMap[key] = f
						}
					} else {
						fieldMap[key] = f
					}
				}
			}
		}
	}
	var fields []*field
	for k := range fieldMap {
		if fieldMap[k].ignored {
			continue
		}
		fields = append(fields, fieldMap[k])
	}
	slices.SortFunc(fields, func(a, b *field) int {
		return strings.Compare(a.key, b.key)
	})
	return fields
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
	if err := e.WriteByte('d'); err != nil {
		return err
	}
	for _, f := range getFieldsForStruct(v) {
		fv := v.FieldByIndex(f.index)
		if isNil(fv) {
			continue
		}
		if fv.Kind() == reflect.Pointer {
			fv = fv.Elem()
		}
		e.encodeString(reflect.ValueOf(f.key))
		e.encode(fv)
	}
	if err := e.WriteByte('e'); err != nil {
		return err
	}
	return nil
}

var marshalerType = reflect.TypeFor[Marshaler]()

func (e *Encoder) encode(v reflect.Value) error {
	if v.Type().Implements(marshalerType) {
		return e.encodeMarshaler(v)
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
		panic("unsupported type passed to encode")
	}
}

func (e *Encoder) Encode(v any) error {
	return e.encode(reflect.ValueOf(v))
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
