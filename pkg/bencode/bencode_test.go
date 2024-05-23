package bencode

import (
	"reflect"
	"testing"
)

func TestByteString_Encode(t *testing.T) {
	tests := []struct {
		name string
		b    ByteString
		want []byte
	}{
		{"0 length", "", []byte("0:")},
		{"1 char", "a", []byte("1:a")},
		{"ends in e", "worde", []byte("5:worde")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.b.Encode(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ByteString.Encode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInteger_Encode(t *testing.T) {
	tests := []struct {
		name string
		i    Integer
		want []byte
	}{
		{"positive", 123, []byte("i123e")},
		{"negative", -456, []byte("i-456e")},
		{"zero", 0, []byte("i0e")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.i.Encode(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Integer.Encode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestList_Encode(t *testing.T) {
	tests := []struct {
		name string
		l    List
		want []byte
	}{
		{"empty list", List{}, []byte("le")},
		{"one item", List{Integer(123)}, []byte("li123ee")},
		{"nested list", List{List{Integer(1), Integer(2)}}, []byte("lli1ei2eee")},
		{"mixed type", List{Integer(123), ByteString("asdf"), List{Integer(456)}}, []byte("li123e4:asdfli456eee")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.l.Encode(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("List.Encode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDictionary_Encode(t *testing.T) {
	tests := []struct {
		name string
		d    Dictionary
		want []byte
	}{
		{"empty dict", Dictionary{}, []byte("de")},
		{"one item", Dictionary{ByteString("key"): ByteString("value")}, []byte("d3:key5:valuee")},
		{"nested dict", Dictionary{ByteString("key"): Dictionary{ByteString("key2"): Integer(0)}}, []byte("d3:keyd4:key2i0eee")},
		{"nested list", Dictionary{
			ByteString("key"): List{Integer(123), Dictionary{ByteString("key2"): Integer(456)}},
		}, []byte("d3:keyli123ed4:key2i456eeee")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.Encode(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Dictionary.Encode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeByteString(t *testing.T) {
	tests := []struct {
		name    string
		b       []byte
		want    ByteString
		wantErr bool
	}{
		{"0 length", []byte("0:"), "", false},
		{"1 char", []byte("1:a"), "a", false},
		{"ends in e", []byte("5:worde"), "worde", false},
		{"malformed length", []byte("5e:word"), "", true},
		{"incorrect length", []byte("5:word"), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeByteString(tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeByteString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DecodeByteString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeInteger(t *testing.T) {
	tests := []struct {
		name    string
		b       []byte
		want    Integer
		wantErr bool
	}{
		{"positive", []byte("i123e"), 123, false},
		{"negative", []byte("i-456e"), -456, false},
		{"zero", []byte("i0e"), 0, false},
		{"malformed number", []byte("i12j23e"), 0, true},
		{"double signed number", []byte("i--123e"), 0, true},
		{"noninitiated number", []byte("123e"), 0, true},
		{"nonterminated number", []byte("i123"), 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeInteger(tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeInteger() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DecodeInteger() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeList(t *testing.T) {
	tests := []struct {
		name    string
		b       []byte
		want    List
		wantErr bool
	}{
		{"empty list", []byte("le"), nil, false},
		{"one item", []byte("li123ee"), List{Integer(123)}, false},
		{"nested list", []byte("lli1ei2eee"), List{List{Integer(1), Integer(2)}}, false},
		{
			"mixed type",
			[]byte("li123e4:asdfli456eee"),
			List{Integer(123), ByteString("asdf"), List{Integer(456)}},
			false,
		},
		{"noninitiated list", []byte("i123ee"), nil, true},
		{"nonterminated list", []byte("li123e"), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeList(tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeList() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DecodeList() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeDictionary(t *testing.T) {
	tests := []struct {
		name    string
		b       []byte
		want    Dictionary
		wantErr bool
	}{
		{"empty dict", []byte("de"), make(Dictionary), false},
		{"one item", []byte("d3:key5:valuee"), Dictionary{ByteString("key"): ByteString("value")}, false},
		{"nested dict", []byte("d3:keyd4:key2i0eee"), Dictionary{ByteString("key"): Dictionary{ByteString("key2"): Integer(0)}}, false},
		{
			"nested list", []byte("d3:keyli123ed4:key2i456eeee"),
			Dictionary{
				ByteString("key"): List{Integer(123), Dictionary{ByteString("key2"): Integer(456)}},
			},
			false,
		},
		{"noninitiated dict", []byte("3:key5:valuee"), nil, true},
		{"nonterminated dict", []byte("d3:key5:value"), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeDictionary(tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeDictionary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DecodeDictionary() = %v, want %v", got, tt.want)
			}
		})
	}
}
