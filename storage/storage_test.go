package storage

import (
	"crypto/rand"
	"crypto/sha1"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/browles/drip/api/metainfo"
)

func Test_checkBlockSize(t *testing.T) {
	info := &metainfo.Info{
		Length:      4*(8*BLOCK_LENGTH+BLOCK_LENGTH/2) - 1000,
		PieceLength: 8*BLOCK_LENGTH + BLOCK_LENGTH/2,
		Pieces:      [][20]byte{{}, {}, {}, {}},
	}
	tests := []struct {
		name    string
		info    *metainfo.Info
		index   int
		begin   int
		length  int
		wantErr bool
	}{
		{"aligned", info, 0, 0, BLOCK_LENGTH, false},
		{"misaligned", info, 0, 1024, BLOCK_LENGTH, true},
		{"short length", info, 0, BLOCK_LENGTH, BLOCK_LENGTH / 2, true},
		{"last block allowed short length", info, 0, 8 * BLOCK_LENGTH, BLOCK_LENGTH / 2, false},
		{"last block of last piece allowed short length", info, 3, 8 * BLOCK_LENGTH, BLOCK_LENGTH/2 - 1000, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkBlockSize(tt.info, tt.index, tt.begin, tt.length); (err != nil) != tt.wantErr {
				t.Errorf("checkBlockSize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func randomBytes(t *testing.T, n int) []byte {
	data := make([]byte, n)
	_, err := rand.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestIntegration(t *testing.T) {
	targetDir := t.TempDir()
	workDir := t.TempDir()
	tempDir := t.TempDir()
	storage := New(targetDir, workDir, tempDir)
	pieces := [][]byte{
		randomBytes(t, 2*BLOCK_LENGTH),
		randomBytes(t, 2*BLOCK_LENGTH),
		randomBytes(t, 2*BLOCK_LENGTH),
		randomBytes(t, 2*BLOCK_LENGTH-1000),
	}
	sha1s := make([][20]byte, len(pieces))
	total := 0
	for i := range pieces {
		sha1s[i] = sha1.Sum(pieces[i])
		total = len(pieces[i])
	}
	info := &metainfo.Info{
		SHA1:        [20]byte{1: 1, 2: 2, 3: 3, 4: 4, 5: 5},
		PieceLength: 2 * BLOCK_LENGTH,
		Pieces:      sha1s,
		Name:        "output_file.txt",
		Length:      total,
	}
	err := storage.AddTorrent(info)
	if err != nil {
		t.Fatal(err)
	}
	torrent := storage.GetTorrent(info.SHA1)
	_, err = storage.GetBlock(info.SHA1, 0, 1024, BLOCK_LENGTH)
	if err == nil {
		t.Fatal("want: block alignment error")
	}
	_, err = storage.GetBlock(info.SHA1, 0, 0, 1024)
	if err == nil {
		t.Fatal("want: block length error")
	}
	_, err = storage.GetBlock(info.SHA1, 0, 0, BLOCK_LENGTH)
	if err == nil {
		t.Fatal("want: piece not coalesced error")
	}
	err = storage.PutBlock(info.SHA1, 0, 1024, pieces[0][:BLOCK_LENGTH/2])
	if err == nil {
		t.Fatal("want: block alignment error")
	}
	err = storage.PutBlock(info.SHA1, 0, 0, pieces[0][:BLOCK_LENGTH/2])
	if err == nil {
		t.Fatal("want: block length error")
	}
	err = storage.PutBlock(info.SHA1, 0, 0, pieces[0][:BLOCK_LENGTH])
	if err != nil {
		t.Fatal(err)
	}
	_, err = storage.GetBlock(info.SHA1, 0, 0, BLOCK_LENGTH)
	if err == nil {
		t.Fatal("want: piece not coalesced error")
	}
	err = storage.PutBlock(info.SHA1, 0, BLOCK_LENGTH, pieces[0][BLOCK_LENGTH:])
	if err != nil {
		t.Fatal(err)
	}
	p := storage.GetPiece(info.SHA1, 0)
	if !p.coalesced {
		t.Fatal("want: coalesced piece")
	}
	_, err = os.Stat(path.Join(workDir, torrent.WorkDir(), "0.piece"))
	if err != nil {
		t.Fatal("want: coalesced piece on disk")
	}
	data, err := storage.GetBlock(info.SHA1, 0, 0, BLOCK_LENGTH)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data, pieces[0][:BLOCK_LENGTH]) {
		t.Fatal("want: correct block data")
	}
}
