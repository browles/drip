package bencode

import (
	"bufio"
	"reflect"
	"strings"
	"testing"
)

func newStringDecoder(s string) *Decoder {
	return &Decoder{Reader: bufio.NewReader(strings.NewReader(s))}
}

func new[T any]() reflect.Value {
	t := reflect.TypeFor[T]()
	return reflect.New(t)
}

type stringUnmarshaler string

func (u *stringUnmarshaler) UnmarshalBencoding(data []byte) error {
	var s string
	if err := Unmarshal(data, &s); err != nil {
		return err
	}
	*u = stringUnmarshaler("prefix-" + s)
	return nil
}

type intUnmarshaler int64

func (u *intUnmarshaler) UnmarshalBencoding(data []byte) error {
	var i int64
	if err := Unmarshal(data, &i); err != nil {
		return err
	}
	*u = intUnmarshaler(i / 1000)
	return nil
}

type listUnmarshaler []string

func (u *listUnmarshaler) UnmarshalBencoding(data []byte) error {
	var s []string
	if err := Unmarshal(data, &s); err != nil {
		return err
	}
	*u = (*u)[:0]
	for _, st := range s {
		*u = append(*u, strings.ToUpper(st))
	}
	return nil
}

type dictUnmarshaler struct {
	A, B, c int
}

func (u *dictUnmarshaler) UnmarshalBencoding(data []byte) error {
	var m map[string]int
	if err := Unmarshal(data, &m); err != nil {
		return err
	}
	*u = dictUnmarshaler{
		A: m["a word"],
		B: m["b word"],
		c: 123,
	}
	return nil
}

func TestDecoder_decodeUnmarshaler(t *testing.T) {
	tests := []struct {
		name    string
		v       reflect.Value
		s       string
		want    any
		wantErr bool
	}{
		{"string unmarshaler", new[stringUnmarshaler](), "3:abc", stringUnmarshaler("prefix-abc"), false},
		{"int unmarshaler", new[intUnmarshaler](), "i12345e", intUnmarshaler(12), false},
		{"list unmarshaler", new[listUnmarshaler](), "l3:abc4:defge", listUnmarshaler{"ABC", "DEFG"}, false},
		{"dict unmarshaler", new[dictUnmarshaler](), "d6:a wordi1e6:b wordi2ee", dictUnmarshaler{A: 1, B: 2, c: 123}, false},
		{
			"unmarshaler field",
			new[struct {
				X int
				B stringUnmarshaler
			}](),
			"d1:b3:abc1:xi123ee",
			struct {
				X int
				B stringUnmarshaler
			}{
				X: 123,
				B: stringUnmarshaler("prefix-abc"),
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			err := d.decode(tt.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("Decoder.decodeUnmarshaler() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got := reflect.Indirect(tt.v).Interface(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decoder.decodeUnmarshaler() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecoder_decodeString(t *testing.T) {
	tests := []struct {
		name    string
		v       reflect.Value
		s       string
		want    any
		wantErr bool
	}{
		{"0 length", new[string](), "0:", "", false},
		{"1 char", new[string](), "1:a", "a", false},
		{"byte slice", new[[]byte](), "3:abc", []byte{'a', 'b', 'c'}, false},
		{"ends in e", new[string](), "5:worde", "worde", false},
		{"malformed length", new[string](), "5e:word", "", true},
		{"incorrect length", new[string](), "5:word", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			err := d.decodeString(reflect.Indirect(tt.v))
			if (err != nil) != tt.wantErr {
				t.Errorf("Decoder.decodeString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got := reflect.Indirect(tt.v).Interface(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decoder.decodeString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecoder_decodeInt(t *testing.T) {
	tests := []struct {
		name    string
		v       reflect.Value
		s       string
		want    any
		wantErr bool
	}{
		{"positive", new[int64](), "i123e", int64(123), false},
		{"negative", new[int32](), "i-456e", int32(-456), false},
		{"zero", new[int](), "i0e", 0, false},
		{"negative zero", new[int](), "i-0e", 0, true},
		{"leading zero", new[int](), "i0123e", 0, true},
		{"negative leading zero", new[int](), "i-0123e", 0, true},
		{"malformed number", new[int](), "i12j23e", 0, true},
		{"double signed number", new[int](), "i--123e", 0, true},
		{"noninitiated number", new[int](), "123e", 0, true},
		{"nonterminated number", new[int](), "i123", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			err := d.decodeInt(reflect.Indirect(tt.v))
			if (err != nil) != tt.wantErr {
				t.Errorf("Decoder.decodeInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got := reflect.Indirect(tt.v).Interface(); got != tt.want {
				t.Errorf("Decoder.decodeInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecoder_decodeUint(t *testing.T) {
	tests := []struct {
		name    string
		v       reflect.Value
		s       string
		want    any
		wantErr bool
	}{
		{"positive", new[uint64](), "i123e", uint64(123), false},
		{"zero", new[uint32](), "i0e", uint32(0), false},
		{"malformed number", new[uint](), "i12j23e", uint(0), true},
		{"negative", new[uint](), "i--123e", uint(0), true},
		{"noninitiated number", new[uint](), "123e", uint(0), true},
		{"nonterminated number", new[uint](), "i123", uint(0), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			err := d.decodeUint(reflect.Indirect(tt.v))
			if (err != nil) != tt.wantErr {
				t.Errorf("Decoder.decodeInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got := reflect.Indirect(tt.v).Interface(); got != tt.want {
				t.Errorf("Decoder.decodeInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecoder_decodeSlice(t *testing.T) {
	tests := []struct {
		name    string
		v       reflect.Value
		s       string
		want    any
		wantErr bool
	}{
		{"empty list", new[[]any](), "le", []any{}, false},
		{"one item", new[[]int64](), "li123ee", []int64{123}, false},
		{"array", new[[3]int64](), "li1ei2ei3ei4ei5ee", [3]int64{1, 2, 3}, false},
		{"nested list", new[[][]int](), "lli1ei2eee", [][]int{{1, 2}}, false},
		{
			"mixed type",
			new[[]any](),
			"li123e4:asdfli456eee",
			[]any{int(123), "asdf", []any{int(456)}},
			false,
		},
		{"noninitiated list", new[[]any](), "i123ee", []any(nil), true},
		{"nonterminated list", new[[]any](), "li123e", []any{int(123)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			err := d.decodeSlice(reflect.Indirect(tt.v))
			if (err != nil) != tt.wantErr {
				t.Errorf("Decoder.decodeSlice() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got := reflect.Indirect(tt.v).Interface(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decoder.decodeSlice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecoder_decodeMap(t *testing.T) {
	tests := []struct {
		name    string
		v       reflect.Value
		s       string
		want    any
		wantErr bool
	}{
		{"empty dict", new[map[string]any](), "de", make(map[string]any), false},
		{"one item", new[map[string]any](), "d3:key5:valuee", map[string]any{"key": "value"}, false},
		{
			"nested dict",
			new[map[string]any](),
			"d3:keyd4:key2i0eee",
			map[string]any{"key": map[string]any{"key2": int(0)}},
			false,
		},
		{
			"nested list",
			new[map[string]any](),
			"d3:keyli123ed4:key2i456eeee",
			map[string]any{
				"key": []any{int(123), map[string]any{"key2": int(456)}},
			},
			false,
		},
		{"noninitiated dict", new[map[string]any](), "3:key5:valuee", map[string]any(nil), true},
		{"nonterminated dict", new[map[string]any](), "d3:key5:value", map[string]any{"key": "value"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			err := d.decodeMap(reflect.Indirect(tt.v))
			if (err != nil) != tt.wantErr {
				t.Errorf("Decoder.decodeMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got := reflect.Indirect(tt.v).Interface(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decoder.decodeMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecoder_decodeStruct(t *testing.T) {
	type simple struct {
		X, Y, z int
	}
	type simpleNested struct {
		A      int
		Nested *simple
	}
	type simpleEmbedded struct {
		simple
		A, Y, Z int
	}
	type deeplyEmbedded struct {
		simpleEmbedded
		A int
	}
	type conflictingEmbedded struct {
		A int
		simpleEmbedded
		simple
	}
	type simpleConflict struct {
		A int
		B int `bencode:"a"`
	}
	type taggedEmbedded struct {
		A      int
		simple `bencode:"embedded"`
	}
	tests := []struct {
		name    string
		v       reflect.Value
		s       string
		want    any
		wantErr bool
	}{
		{"empty struct", new[struct{}](), "de", struct{}{}, false},
		{
			"tags",
			new[struct {
				A           int `bencode:"a_tag"`
				NoTag       int
				notExported int
				Ignored     int `bencode:"-"`
			}](),
			"d5:a_tagi1e5:notagi2ee",
			struct {
				A           int `bencode:"a_tag"`
				NoTag       int
				notExported int
				Ignored     int `bencode:"-"`
			}{
				1, 2, 0, 0,
			},
			false,
		},
		{
			"nested struct",
			new[simpleNested](),
			"d1:ai1e6:nestedd1:xi2e1:yi3eee",
			simpleNested{
				A:      1,
				Nested: &simple{X: 2, Y: 3, z: 0},
			},
			false,
		},
		{
			"nil nested struct",
			new[simpleNested](),
			"d1:ai1ee",
			simpleNested{
				A:      1,
				Nested: nil,
			},
			false,
		},
		{
			"embedded struct",
			new[simpleEmbedded](),
			"d1:ai22e1:xi1e1:yi33e1:zi44ee",
			simpleEmbedded{
				simple: simple{X: 1, Y: 0, z: 0},
				A:      22,
				Y:      33,
				Z:      44,
			},
			false,
		},
		{
			"deeply embedded struct",
			new[deeplyEmbedded](),
			"d1:ai99e1:xi1e1:yi33e1:zi44ee",
			deeplyEmbedded{
				simpleEmbedded: simpleEmbedded{
					simple: simple{X: 1, Y: 0, z: 0},
					A:      0,
					Y:      33,
					Z:      44,
				},
				A: 99,
			},
			false,
		},
		{
			"conflicting field names embedded struct",
			new[conflictingEmbedded](),
			"d1:ai11e1:xi55e1:zi44ee",
			conflictingEmbedded{
				A: 11,
				simpleEmbedded: simpleEmbedded{
					simple: simple{X: 0, Y: 0, z: 0},
					A:      0,
					Y:      0,
					Z:      44,
				},
				simple: simple{X: 55, Y: 0, z: 0},
			},
			false,
		},
		{
			"conflicting field bencoding names",
			new[simpleConflict](),
			"d1:ai2ee",
			simpleConflict{
				A: 0,
				B: 2,
			},
			false,
		},
		{
			"tagged embedded struct",
			new[taggedEmbedded](),
			"d1:ai22e8:embeddedd1:xi1e1:yi2eee",
			taggedEmbedded{
				A:      22,
				simple: simple{X: 1, Y: 2, z: 0},
			},
			false,
		},
		{
			"skipped fields",
			new[taggedEmbedded](),
			"d1:ai22e1:b5:abcde1:ci12345e8:embeddedd1:xi1e1:yi2eee",
			taggedEmbedded{
				A:      22,
				simple: simple{X: 1, Y: 2, z: 0},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			err := d.decodeStruct(reflect.Indirect(tt.v))
			if (err != nil) != tt.wantErr {
				t.Errorf("Decoder.decodeStruct() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got := reflect.Indirect(tt.v).Interface(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decoder.decodeStruct() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecoder_decodeInterface(t *testing.T) {
	tests := []struct {
		name    string
		v       reflect.Value
		s       string
		want    any
		wantErr bool
	}{
		{"non-nil interface", reflect.ValueOf(any(&[]int32{})), "li123ei456ee", []int32{123, 456}, false},
		{"inferred int", new[any](), "i123e", int(123), false},
		{"inferred string", new[any](), "3:abc", "abc", false},
		{"inferred slice", new[[]any](), "li123ee", []any{int(123)}, false},
		{"inferred map", new[any](), "d3:key5:valuee", map[string]any{"key": "value"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			err := d.decodeInterface(tt.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("Decoder.decodeInterface() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got := reflect.Indirect(tt.v).Interface(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Decoder.decodeInterface() = %v, want %v", got, tt.want)
			}
		})
	}
}
