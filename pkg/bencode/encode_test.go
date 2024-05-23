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
	e := Encoder{b}
	return &stringEncoder{b, e}
}

func Test_Encoder_encodeString(t *testing.T) {
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
			be.e.encodeString(tt.s)
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_Encoder_encodeInt(t *testing.T) {
	tests := []struct {
		name string
		i    int64
		want string
	}{
		{"positive", 123, "i123e"},
		{"negative", -456, "i-456e"},
		{"zero", 0, "i0e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newStringEncoder()
			be.e.encodeInt(tt.i)
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_Encoder_encodeList(t *testing.T) {
	tests := []struct {
		name string
		l    []any
		want string
	}{
		{"empty list", []any{}, "le"},
		{"one item", []any{int(123)}, "li123ee"},
		{"nested list", []any{[]any{int(1), int(2)}}, "lli1ei2eee"},
		{"mixed type", []any{int(123), "asdf", []any{int(456)}}, "li123e4:asdfli456eee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newStringEncoder()
			be.e.encodeList(tt.l)
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_Encoder_encodeDictionary(t *testing.T) {
	tests := []struct {
		name string
		d    map[string]any
		want string
	}{
		{"empty dict", map[string]any{}, "de"},
		{"one item", map[string]any{"key": "value"}, "d3:key5:valuee"},
		{"nested dict", map[string]any{"key": map[string]any{"key2": 0}}, "d3:keyd4:key2i0eee"},
		{"nested list", map[string]any{
			string("key"): []any{123, map[string]any{"key2": 456}},
		}, "d3:keyli123ed4:key2i456eeee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newStringEncoder()
			be.e.encodeDictionary(tt.d)
			if got := be.b.String(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
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

func TestMarshal(t *testing.T) {
	tests := []struct {
		name    string
		v       any
		want    string
		wantErr bool
	}{
		{"custom marshaler", &bencodingMarshaler{"asdf"}, "4:asdf", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("Encode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(string(got), tt.want) {
				t.Errorf("Encode() = %v, want %v", string(got), tt.want)
			}
		})
	}
}
