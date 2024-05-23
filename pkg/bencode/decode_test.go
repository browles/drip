package bencode

import (
	"bufio"
	"reflect"
	"strings"
	"testing"
)

func newStringDecoder(s string) *Decoder {
	return &Decoder{*bufio.NewReader(strings.NewReader(s))}
}

func Test_decoder_decodeString(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		want    string
		wantErr bool
	}{
		{"0 length", "0:", "", false},
		{"1 char", "1:a", "a", false},
		{"ends in e", "5:worde", "worde", false},
		{"malformed length", "5e:word", "", true},
		{"incorrect length", "5:word", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			got, err := d.decodeString()
			if (err != nil) != tt.wantErr {
				t.Errorf("decoder.decodeString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("decoder.decodeString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_decoder_decodeInt(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		want    int64
		wantErr bool
	}{
		{"positive", "i123e", 123, false},
		{"negative", "i-456e", -456, false},
		{"zero", "i0e", 0, false},
		{"malformed number", "i12j23e", 0, true},
		{"double signed number", "i--123e", 0, true},
		{"noninitiated number", "123e", 0, true},
		{"nonterminated number", "i123", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			got, err := d.decodeInt()
			if (err != nil) != tt.wantErr {
				t.Errorf("decoder.decodeInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("decoder.decodeInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_decoder_decodeList(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		want    []any
		wantErr bool
	}{
		{"empty list", "le", nil, false},
		{"one item", "li123ee", []any{int64(123)}, false},
		{"nested list", "lli1ei2eee", []any{[]any{int64(1), int64(2)}}, false},
		{
			"mixed type",
			"li123e4:asdfli456eee",
			[]any{int64(123), "asdf", []any{int64(456)}},
			false,
		},
		{"noninitiated list", "i123ee", nil, true},
		{"nonterminated list", "li123e", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			got, err := d.decodeList()
			if (err != nil) != tt.wantErr {
				t.Errorf("decoder.decodeList() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("decoder.decodeList() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_decoder_decodeDictionary(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		want    map[string]any
		wantErr bool
	}{
		{"empty dict", "de", make(map[string]any), false},
		{"one item", "d3:key5:valuee", map[string]any{"key": "value"}, false},
		{"nested dict", "d3:keyd4:key2i0eee", map[string]any{"key": map[string]any{"key2": int64(0)}}, false},
		{
			"nested list", "d3:keyli123ed4:key2i456eeee",
			map[string]any{
				"key": []any{int64(123), map[string]any{"key2": int64(456)}},
			},
			false,
		},
		{"noninitiated dict", "3:key5:valuee", nil, true},
		{"nonterminated dict", "d3:key5:value", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newStringDecoder(tt.s)
			got, err := d.decodeDictionary()
			if (err != nil) != tt.wantErr {
				t.Errorf("decoder.decodeDictionary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("decoder.decodeDictionary() = %v, want %v", got, tt.want)
			}
		})
	}
}
