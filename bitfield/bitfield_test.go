package bitfield

import (
	"reflect"
	"testing"
)

func TestBitfield_Has(t *testing.T) {
	tests := []struct {
		name string
		b    Bitfield
		i    []int
		want bool
	}{
		{
			"contains",
			Bitfield{0b10101010, 0b11001100},
			[]int{0, 2, 4, 6, 8, 9, 12, 13},
			true,
		},
		{
			"does not contain",
			Bitfield{0b10101010, 0b11001100},
			[]int{1, 3, 5, 7, 10, 11, 14, 15, 16, 32},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, i := range tt.i {
				if got := tt.b.Has(i); got != tt.want {
					t.Errorf("Bitfield.Has() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestBitfield_Add(t *testing.T) {
	tests := []struct {
		name string
		i    []int
		want Bitfield
	}{
		{
			"add",
			[]int{0, 2, 4, 6, 8, 9, 12, 13},
			Bitfield{0b10101010, 0b11001100},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(16)
			for _, i := range tt.i {
				b.Add(i)
			}
			if !reflect.DeepEqual(b, tt.want) {
				t.Errorf("Bitfield.Add() = %v, want %v", b, tt.want)
			}
		})
	}
}

func TestBitfield_Items(t *testing.T) {
	tests := []struct {
		name string
		b    Bitfield
		want []int
	}{
		{
			"items",
			Bitfield{0b10101010, 0b11001100},
			[]int{0, 2, 4, 6, 8, 9, 12, 13},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.b.Items(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Bitfield.Items() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBitfield_Difference(t *testing.T) {
	tests := []struct {
		name string
		b    Bitfield
		a    Bitfield
		want Bitfield
	}{
		{"empty", Bitfield{}, Bitfield{0b01, 0x1}, Bitfield{}},
		{"longer", Bitfield{0x00, 0xff}, Bitfield{0xff}, Bitfield{0x00, 0xff}},
		{"shorter", Bitfield{0xff}, Bitfield{0x00, 0xff}, Bitfield{0xff}},
		{"difference", Bitfield{0b11001100, 0b11110000, 0xff}, Bitfield{0b10101010, 0b01101101}, Bitfield{0b01000100, 0b10010000, 0xff}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.b.Difference(tt.a); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Bitfield.Difference() = %v, want %v", got, tt.want)
			}
		})
	}
}
