package storage

import (
	"crypto/rand"
	"crypto/sha1"
	"os"
	"path/filepath"
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

func TestIntegrationReloadingPartialTorrents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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
		total += len(pieces[i])
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
	// Piece has no data
	if _, err = storage.GetBlock(info.SHA1, 0, 0, BLOCK_LENGTH); err == nil {
		t.Fatal("want: piece not coalesced error")
	}
	// Invalid block puts
	if err = storage.PutBlock(info.SHA1, 0, 1024, pieces[0][:BLOCK_LENGTH/2]); err == nil {
		t.Fatal("want: block alignment error")
	}
	if err = storage.PutBlock(info.SHA1, 0, 0, pieces[0][:BLOCK_LENGTH/2]); err == nil {
		t.Fatal("want: block length error")
	}
	// Piece has partial data
	if err = storage.PutBlock(info.SHA1, 0, 0, pieces[0][:BLOCK_LENGTH]); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.GetBlock(info.SHA1, 0, 0, BLOCK_LENGTH); err == nil {
		t.Fatal("want: piece not coalesced error")
	}
	if err = storage.PutBlock(info.SHA1, 0, BLOCK_LENGTH, pieces[0][BLOCK_LENGTH:]); err != nil {
		t.Fatal(err)
	}
	// Piece has complete data
	if !storage.GetPiece(info.SHA1, 0).coalesced {
		t.Fatalf("want: coalesced piece: %d", 0)
	}
	if _, err = os.Stat(filepath.Join(workDir, torrent.WorkDir(), "0.piece")); err != nil {
		t.Fatalf("want: coalesced piece on disk: %s", err)
	}
	data, err := storage.GetBlock(info.SHA1, 0, 0, BLOCK_LENGTH)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data, pieces[0][:BLOCK_LENGTH]) {
		t.Fatal("want: correct block data from piece")
	}
	// Reload partial torrent
	delete(storage.torrents, info.SHA1)
	err = storage.AddTorrent(info)
	if err != nil {
		t.Fatal(err)
	}
	torrent = storage.GetTorrent(info.SHA1)
	if !storage.GetPiece(info.SHA1, 0).coalesced {
		t.Fatal("want: coalesced piece")
	}
	if storage.GetPiece(info.SHA1, 1).coalesced {
		t.Fatal("want: non-coalesced piece")
	}
	// Put rest of pieces (without trigger torrent coalesce)
	for i := 1; i < len(pieces); i++ {
		piece := torrent.pieces[i]
		piece.putBlock(0, pieces[i][:BLOCK_LENGTH])
		piece.putBlock(BLOCK_LENGTH, pieces[i][BLOCK_LENGTH:])
		if err = storage.coalesceBlocks(torrent, piece); err != nil {
			t.Fatal(err)
		}
	}
	// Pieces have complete data
	for i := range len(pieces) {
		if !storage.GetPiece(info.SHA1, i).coalesced {
			t.Fatalf("want: coalesced piece: %d", i)
		}
	}
	// Reload and coalesce complete but non-coalesced torrent
	delete(storage.torrents, info.SHA1)
	err = storage.AddTorrent(info)
	if err != nil {
		t.Fatal(err)
	}
	torrent = storage.GetTorrent(info.SHA1)
	// Torrent has complete data
	if !torrent.coalesced {
		t.Fatal("want: coalesced torrent")
	}
	stat, err := os.Stat(filepath.Join(targetDir, torrent.FileName()))
	if err != nil {
		t.Fatalf("want: coalesced torrent on disk: %s", err)
	}
	if stat.Size() != int64(info.Length) {
		t.Fatalf("want: coalesced torrent size on disk: %d != %d", stat.Size(), info.Length)
	}
	if files, err := os.ReadDir(filepath.Join(workDir, torrent.WorkDir())); err == nil || len(files) > 0 {
		t.Error("want: cleaned up pieces on disk")
	}
	// Reload complete torrent
	delete(storage.torrents, info.SHA1)
	err = storage.AddTorrent(info)
	if err != nil {
		t.Fatal(err)
	}
	torrent = storage.GetTorrent(info.SHA1)
	if !torrent.coalesced {
		t.Fatal("want: coalesced torrent")
	}
	for i := range len(pieces) {
		if !storage.GetPiece(info.SHA1, i).coalesced {
			t.Fatal("want: coalesced piece")
		}
	}
}

func TestIntegrationSingleFileTorrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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
		total += len(pieces[i])
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
	for i := 0; i < len(pieces); i++ {
		if err = storage.PutBlock(info.SHA1, i, 0, pieces[i][:BLOCK_LENGTH]); err != nil {
			t.Fatal(err)
		}
		if err = storage.PutBlock(info.SHA1, i, BLOCK_LENGTH, pieces[i][BLOCK_LENGTH:]); err != nil {
			t.Fatal(err)
		}
	}
	if !torrent.coalesced {
		t.Fatal("want: coalesced torrent")
	}
	stat, err := os.Stat(filepath.Join(targetDir, torrent.FileName()))
	if err != nil {
		t.Fatalf("want: coalesced torrent on disk: %s", err)
	}
	if stat.Size() != int64(info.Length) {
		t.Fatalf("want: coalesced torrent size on disk: %d != %d", stat.Size(), info.Length)
	}
	if files, err := os.ReadDir(filepath.Join(workDir, torrent.WorkDir())); err == nil || len(files) > 0 {
		t.Error("want: cleaned up pieces on disk")
	}
	checkBlock := func(index, begin, length int) {
		data, err := storage.GetBlock(info.SHA1, index, begin, length)
		if err != nil {
			t.Fatalf("want: block data from torrent, index=%d begin=%d length=%d: %s", index, begin, length, err)
		}
		want := pieces[index][begin : begin+length]
		if !reflect.DeepEqual(data, want) {
			t.Errorf("want: correct block data from torrent, index=%d begin=%d length=%d", index, begin, length)
		}
	}
	for i := 0; i < len(pieces); i++ {
		checkBlock(i, 0, BLOCK_LENGTH)
		checkBlock(i, BLOCK_LENGTH, len(pieces[i])-BLOCK_LENGTH)
	}
	checkBlock(0, 0, 2*BLOCK_LENGTH)
	checkBlock(0, 0, 3*BLOCK_LENGTH/2)
}

func TestIntegrationMultiFileTorrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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
		total += len(pieces[i])
	}
	info := &metainfo.Info{
		SHA1:        [20]byte{1: 1, 2: 2, 3: 3, 4: 4, 5: 5},
		PieceLength: 2 * BLOCK_LENGTH,
		Pieces:      sha1s,
		Name:        "output_dir",
		Length:      total,
		Files: []*metainfo.File{
			{Length: (3 * BLOCK_LENGTH) / 2, Path: []string{"a.txt"}},
			{Length: 2 * BLOCK_LENGTH, Path: []string{"b", "b.txt"}},
			{Length: total - 2*BLOCK_LENGTH - (3*BLOCK_LENGTH)/2, Path: []string{"b", "c", "c.txt"}},
		},
	}
	err := storage.AddTorrent(info)
	if err != nil {
		t.Fatal(err)
	}
	torrent := storage.GetTorrent(info.SHA1)
	for i := 0; i < len(pieces); i++ {
		if err = storage.PutBlock(info.SHA1, i, 0, pieces[i][:BLOCK_LENGTH]); err != nil {
			t.Fatal(err)
		}
		if err = storage.PutBlock(info.SHA1, i, BLOCK_LENGTH, pieces[i][BLOCK_LENGTH:]); err != nil {
			t.Fatal(err)
		}
	}
	if !torrent.coalesced {
		t.Fatal("want: coalesced torrent")
	}
	if _, err = os.Stat(filepath.Join(targetDir, torrent.FileName())); err != nil {
		t.Fatal("want: coalesced torrent on disk")
	}
	for _, file := range info.Files {
		fp := append([]string{targetDir, torrent.FileName()}, file.Path...)
		stat, err := os.Stat(filepath.Join(fp...))
		if err != nil {
			t.Fatalf("want: coalesced torrent file on disk: %v", file.Path)
		}
		if stat.Size() != int64(file.Length) {
			t.Fatalf("want: coalesced torrent file size on disk: %d != %d", stat.Size(), file.Length)
		}
	}
	if files, err := os.ReadDir(filepath.Join(workDir, torrent.WorkDir())); err == nil || len(files) > 0 {
		t.Error("want: cleaned up pieces on disk")
	}
	checkBlock := func(index, begin, length int) {
		data, err := storage.GetBlock(info.SHA1, index, begin, length)
		if err != nil {
			t.Fatalf("want: block data from torrent, index=%d begin=%d length=%d: %s", index, begin, length, err)
		}
		want := pieces[index][begin : begin+length]
		if !reflect.DeepEqual(data, want) {
			t.Errorf("want: correct block data from torrent, index=%d begin=%d length=%d", index, begin, length)
		}
	}
	for i := 0; i < len(pieces); i++ {
		checkBlock(i, 0, BLOCK_LENGTH)
		checkBlock(i, BLOCK_LENGTH, len(pieces[i])-BLOCK_LENGTH)
	}
}
