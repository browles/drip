package bitfield

import (
	"reflect"
	"testing"
)

func TestBitfield_Get(t *testing.T) {
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
				if got := tt.b.Get(i); got != tt.want {
					t.Errorf("Bitfield.Get() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestBitfield_Set(t *testing.T) {
	tests := []struct {
		name string
		i    []int
		want Bitfield
	}{
		{
			"set",
			[]int{0, 2, 4, 6, 8, 9, 12, 13},
			Bitfield{0b10101010, 0b11001100},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Bitfield{}
			for _, i := range tt.i {
				b.Set(i)
			}
			if !reflect.DeepEqual(b, tt.want) {
				t.Errorf("Bitfield.Set() = %v, want %v", b, tt.want)
			}
		})
	}
}

func TestBitfield_All(t *testing.T) {
	tests := []struct {
		name string
		b    Bitfield
		want []int
	}{
		{
			"all",
			Bitfield{0b10101010, 0b11001100},
			[]int{0, 2, 4, 6, 8, 9, 12, 13},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.b.All(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Bitfield.All() = %v, want %v", got, tt.want)
			}
		})
	}
}
