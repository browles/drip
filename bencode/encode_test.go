package bencode

import (
	"bytes"
	"reflect"
	"strconv"
	"testing"
)

type stringEncoder struct {
	b *bytes.Buffer
	e Encoder
}

func newStringEncoder() *stringEncoder {
	b := &bytes.Buffer{}
	e := Encoder{Writer: b}
	return &stringEncoder{b, e}
}

type bencodingMarshaler struct {
	s string
}

func (b *bencodingMarshaler) MarshalBencoding() ([]byte, error) {
	var bu bytes.Buffer
	bu.WriteString(strconv.Itoa(len(b.s)))
	bu.WriteByte(':')
	bu.WriteString(b.s)
	return bu.Bytes(), nil
}

func TestEncoder_encodeMarshaler(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"marshaler", &bencodingMarshaler{"asdf"}, "4:asdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newStringEncoder()
			be.e.encodeMarshaler(reflect.ValueOf(tt.v))
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Encoder.encodeMarshaler() = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func TestEncoder_encodeString(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{"0 length", "", "0:"},
		{"1 char", "a", "1:a"},
		{"ends in e", "worde", "5:worde"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newStringEncoder()
			be.e.encodeString(reflect.ValueOf(tt.s))
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Encoder.encodeString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncoder_encodeBytes(t *testing.T) {
	tests := []struct {
		name string
		s    []byte
		want string
	}{
		{"0 length", []byte{}, "0:"},
		{"1 char", []byte("a"), "1:a"},
		{"ends in e", []byte("worde"), "5:worde"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newStringEncoder()
			be.e.encodeBytes(reflect.ValueOf(tt.s))
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Encoder.encodeBytes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncoder_encodeInt(t *testing.T) {
	tests := []struct {
		name string
		i    any
		want string
	}{
		{"positive", int(123), "i123e"},
		{"negative", int16(-456), "i-456e"},
		{"zero", int64(0), "i0e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newStringEncoder()
			be.e.encodeInt(reflect.ValueOf(tt.i))
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Encoder.encodeInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncoder_encodeUint(t *testing.T) {
	tests := []struct {
		name string
		i    any
		want string
	}{
		{"positive", uint(123), "i123e"},
		{"zero", uint64(0), "i0e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newStringEncoder()
			be.e.encodeUint(reflect.ValueOf(tt.i))
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Encoder.encodeUint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncoder_encodeSlice(t *testing.T) {
	tests := []struct {
		name string
		l    []any
		want string
	}{
		{"nil slice", nil, ""},
		{"empty slice", []any{}, "le"},
		{"one item", []any{123}, "li123ee"},
		{"nested slice", []any{[]any{1, 2}}, "lli1ei2eee"},
		{"mixed type", []any{123, "asdf", []any{456}}, "li123e4:asdfli456eee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newStringEncoder()
			be.e.encodeSlice(reflect.ValueOf(tt.l))
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Encoder.encodeSlice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncoder_encodeMap(t *testing.T) {
	tests := []struct {
		name string
		d    map[string]any
		want string
	}{
		{"nil map", nil, ""},
		{"empty map", map[string]any{}, "de"},
		{"one item", map[string]any{"key": "value"}, "d3:key5:valuee"},
		{"unsorted keys", map[string]any{"b": 2, "c": 3, "a": 1}, "d1:ai1e1:bi2e1:ci3ee"},
		{"nested map", map[string]any{"key": map[string]any{"key2": 0}}, "d3:keyd4:key2i0eee"},
		{"nested slice", map[string]any{"key": []any{123, map[string]any{"key2": 456}}}, "d3:keyli123ed4:key2i456eeee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newStringEncoder()
			be.e.encodeMap(reflect.ValueOf(tt.d))
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Encoder.encodeMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncoder_encodeStruct(t *testing.T) {
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
	type recursive struct {
		A *recursive
		X int
	}
	cyclical := recursive{X: 1}
	cyclical.A = &cyclical
	tests := []struct {
		name    string
		s       any
		want    string
		wantErr bool
	}{
		{"empty struct", struct{}{}, "de", false},
		{
			"tags",
			struct {
				A           int `bencode:"a_tag"`
				NoTag       int
				notExported int
				Ignored     int `bencode:"-"`
			}{
				1, 2, 3, 4,
			},
			"d5:a_tagi1e5:notagi2ee",
			false,
		},
		{
			"nested struct",
			simpleNested{
				A:      1,
				Nested: &simple{X: 2, Y: 3, z: 4},
			},
			"d1:ai1e6:nestedd1:xi2e1:yi3eee",
			false,
		},
		{
			"nil nested struct",
			simpleNested{
				A:      1,
				Nested: nil,
			},
			"d1:ai1ee",
			false,
		},
		{
			"embedded struct",
			simpleEmbedded{
				simple: simple{X: 1, Y: 2, z: 3},
				A:      22,
				Y:      33,
				Z:      44,
			},
			"d1:ai22e1:xi1e1:yi33e1:zi44ee",
			false,
		},
		{
			"deeply embedded struct",
			deeplyEmbedded{
				simpleEmbedded: simpleEmbedded{
					simple: simple{X: 1, Y: 2, z: 3},
					A:      22,
					Y:      33,
					Z:      44,
				},
				A: 99,
			},
			"d1:ai99e1:xi1e1:yi33e1:zi44ee",
			false,
		},
		{
			"conflicting field names embedded struct",
			conflictingEmbedded{
				A: 11,
				simpleEmbedded: simpleEmbedded{
					simple: simple{X: 1, Y: 2, z: 3},
					A:      22,
					Y:      33,
					Z:      44,
				},
				simple: simple{X: 55, Y: 66, z: 77},
			},
			"d1:ai11e1:xi55e1:zi44ee",
			false,
		},
		{
			"conflicting field bencoding names",
			simpleConflict{
				A: 1,
				B: 2,
			},
			"d1:ai2ee",
			false,
		},
		{
			"tagged embedded struct",
			taggedEmbedded{
				A:      22,
				simple: simple{X: 1, Y: 2, z: 3},
			},
			"d1:ai22e8:embeddedd1:xi1e1:yi2eee",
			false,
		},
		{
			"cyclical struct",
			cyclical,
			"",
			true,
		},
		{
			"omitempty",
			struct {
				A int
				B int `bencode:",omitempty"`
			}{A: 1, B: 0},
			"d1:ai1ee",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newStringEncoder()
			err := be.e.encodeStruct(reflect.ValueOf(tt.s))
			if (err != nil) != tt.wantErr {
				t.Errorf("Encode.encodeStruct() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Encoder.encodeStruct() = %v, want %v", got, tt.want)
			}
		})
	}
}
