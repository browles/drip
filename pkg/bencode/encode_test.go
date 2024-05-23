package bencode

import (
	"bytes"
	"reflect"
	"testing"
)

type bytesEncoder struct {
	b *bytes.Buffer
	e Encoder
}

func newBytesEncoder() *bytesEncoder {
	b := &bytes.Buffer{}
	e := Encoder{b}
	return &bytesEncoder{b, e}
}

func Test_encoder_encodeString(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want []byte
	}{
		{"0 length", "", []byte("0:")},
		{"1 char", "a", []byte("1:a")},
		{"ends in e", "worde", []byte("5:worde")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newBytesEncoder()
			be.e.encodeString(tt.s)
			if got := be.b.Bytes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_encoder_encodeInt(t *testing.T) {
	tests := []struct {
		name string
		i    int64
		want []byte
	}{
		{"positive", 123, []byte("i123e")},
		{"negative", -456, []byte("i-456e")},
		{"zero", 0, []byte("i0e")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newBytesEncoder()
			be.e.encodeInt(tt.i)
			if got := be.b.Bytes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_encoder_encodeList(t *testing.T) {
	tests := []struct {
		name string
		l    []any
		want []byte
	}{
		{"empty list", []any{}, []byte("le")},
		{"one item", []any{int(123)}, []byte("li123ee")},
		{"nested list", []any{[]any{int(1), int(2)}}, []byte("lli1ei2eee")},
		{"mixed type", []any{int(123), string("asdf"), []any{int(456)}}, []byte("li123e4:asdfli456eee")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newBytesEncoder()
			be.e.encodeList(tt.l)
			if got := be.b.Bytes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_encoder_encodeDictionary(t *testing.T) {
	tests := []struct {
		name string
		d    map[string]any
		want []byte
	}{
		{"empty dict", map[string]any{}, []byte("de")},
		{"one item", map[string]any{string("key"): string("value")}, []byte("d3:key5:valuee")},
		{"nested dict", map[string]any{string("key"): map[string]any{string("key2"): 0}}, []byte("d3:keyd4:key2i0eee")},
		{"nested list", map[string]any{
			string("key"): []any{123, map[string]any{string("key2"): 456}},
		}, []byte("d3:keyli123ed4:key2i456eeee")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			be := newBytesEncoder()
			be.e.encodeDictionary(tt.d)
			if got := be.b.Bytes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
