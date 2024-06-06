package metainfo

import (
	"crypto/sha1"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestTime_MarshalBencoding(t *testing.T) {
	tests := []struct {
		name    string
		time    *Time
		want    string
		wantErr bool
	}{
		{"unix time", &Time{time.Unix(1717651912, 0)}, "i1717651912e", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.time.MarshalBencoding()
			if (err != nil) != tt.wantErr {
				t.Errorf("Time.MarshalBencoding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(string(got), tt.want) {
				t.Errorf("Time.MarshalBencoding() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTime_UnmarshalBencoding(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    *Time
		wantErr bool
	}{
		{"unix time", "i1717651912e", &Time{time.Unix(1717651912, 0)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ti := &Time{}
			if err := ti.UnmarshalBencoding([]byte(tt.data)); (err != nil) != tt.wantErr {
				t.Errorf("Time.UnmarshalBencoding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(ti, tt.want) {
				t.Errorf("Time.UnmarshalBencoding() = %+v, want %+v", ti, tt.want)
			}
		})
	}
}

func TestInfo_UnmarshalBencoding(t *testing.T) {
	abcSha1 := sha1.Sum([]byte("abc"))
	defSha1 := sha1.Sum([]byte("def"))
	tests := []struct {
		name    string
		data    string
		want    *Info
		wantErr bool
	}{
		{
			"unmarshal",
			fmt.Sprintf(
				"d6:lengthi2048e4:name8:file.txt12:piece lengthi1024e6:pieces40:%see",
				string(abcSha1[:])+string(defSha1[:]),
			),
			&Info{
				PieceLength: 1024,
				Pieces:      Pieces{abcSha1, defSha1},
				Name:        "file.txt",
				Length:      2048,
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := &Info{}
			if err := i.UnmarshalBencoding([]byte(tt.data)); (err != nil) != tt.wantErr {
				t.Errorf("Info.UnmarshalBencoding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			tt.want.SHA1 = sha1.Sum([]byte(tt.data))
			if !reflect.DeepEqual(i, tt.want) {
				t.Errorf("Info.UnmarshalBencoding() = %+v, want %+v", i, tt.want)
			}
		})
	}
}

func TestPieces_MarshalBencoding(t *testing.T) {
	abcSha1 := sha1.Sum([]byte("abc"))
	defSha1 := sha1.Sum([]byte("def"))
	tests := []struct {
		name    string
		p       Pieces
		want    string
		wantErr bool
	}{
		{
			"marshal",
			Pieces{abcSha1, defSha1},
			fmt.Sprintf("40:%s", string(abcSha1[:])+string(defSha1[:])),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.MarshalBencoding()
			if (err != nil) != tt.wantErr {
				t.Errorf("Pieces.MarshalBencoding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(string(got), tt.want) {
				t.Errorf("Pieces.MarshalBencoding() = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func TestPieces_UnmarshalBencoding(t *testing.T) {
	abcSha1 := sha1.Sum([]byte("abc"))
	defSha1 := sha1.Sum([]byte("def"))
	tests := []struct {
		name    string
		data    string
		want    Pieces
		wantErr bool
	}{
		{
			"unmarshal",
			fmt.Sprintf("40:%s", string(abcSha1[:])+string(defSha1[:])),
			Pieces{abcSha1, defSha1},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p Pieces
			err := p.UnmarshalBencoding([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Errorf("Pieces.UnmarshalBencoding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(p, tt.want) {
				t.Errorf("Pieces.UnmarshalBencoding() = %+v, want %+v", p, tt.want)
			}
		})
	}
}
