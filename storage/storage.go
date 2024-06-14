package storage

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sync"

	"github.com/browles/drip/api/metainfo"
	"github.com/browles/drip/bitfield"
)

type Storage struct {
	TargetDir   string
	WorkDir     string
	TempDir     string
	BlockLength int

	mu       sync.Mutex
	torrents map[[20]byte]*Torrent
}

type Torrent struct {
	Info *metainfo.Info
	Done chan struct{}

	mu             sync.Mutex
	Bitfield       bitfield.Bitfield
	Complete       bool
	completePieces int
	pieces         []*Piece
}

func (t *Torrent) WorkDir() string {
	return hex.EncodeToString(t.Info.SHA1[:])
}

func (t *Torrent) Filename() string {
	return t.Info.Name
}

type Piece struct {
	SHA1  [20]byte
	Done  chan struct{}
	Index int

	mu             sync.Mutex
	Complete       bool
	completeBlocks int
	blocks         []*Block
}

func (p *Piece) Filename() string {
	return fmt.Sprintf("%d.piece", p.Index)
}

type Block struct {
	Index int
	Begin int
	Data  []byte
}

func New(targetDir, workDir, tempDir string, blockLength int) *Storage {
	return &Storage{
		TargetDir:   targetDir,
		WorkDir:     workDir,
		TempDir:     tempDir,
		BlockLength: blockLength,
		torrents:    make(map[[20]byte]*Torrent),
	}
}

func (s *Storage) AddTorrent(info *metainfo.Info) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.torrents[info.SHA1]; ok {
		return nil
	}
	t := &Torrent{
		Info:   info,
		pieces: make([]*Piece, len(info.Pieces)),
		Done:   make(chan struct{}),
	}
	s.torrents[info.SHA1] = t
	f, err := os.Open(path.Join(s.TargetDir, t.Filename()))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		sha1Digest := sha1.New()
		if _, err := io.Copy(sha1Digest, f); err != nil {
			return err
		}
		var hash [20]byte
		sha1Digest.Sum(hash[:])
		if hash != t.Info.SHA1 {
			return fmt.Errorf("torrent SHA1 does not match target: got %s != want %s",
				hex.EncodeToString(hash[:]),
				hex.EncodeToString(t.Info.SHA1[:]),
			)
		}
		t.Complete = true
		close(t.Done)
		for i := len(t.Info.Pieces) - 1; i >= 0; i-- {
			t.Bitfield.Add(i)
		}
		return nil
	}
	direntries, err := os.ReadDir(path.Join(s.WorkDir, t.WorkDir()))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	for _, dr := range direntries {
		var index int
		_, err := fmt.Sscanf(dr.Name(), "%d.piece", &index)
		if err != nil {
			continue
		}
		p := s.GetPiece(t.Info.SHA1, index)
		p.Complete = true
		close(p.Done)
		t.Bitfield.Add(index)
	}
	return nil
}

func (s *Storage) GetTorrent(infohash [20]byte) *Torrent {
	s.mu.Lock()
	defer s.mu.Unlock()
	torrent, ok := s.torrents[infohash]
	if !ok {
		panic(errors.New("GetTorrent: unknown info hash"))
	}
	return torrent
}

func (s *Storage) GetPiece(infoHash [20]byte, index int) *Piece {
	torrent := s.GetTorrent(infoHash)
	torrent.mu.Lock()
	defer torrent.mu.Unlock()
	if torrent.pieces[index] != nil {
		return torrent.pieces[index]
	}
	pieceLength := torrent.Info.PieceLength
	if index == len(torrent.Info.Pieces)-1 {
		pieceLength = torrent.Info.Length - index*torrent.Info.PieceLength
	}
	nBlocks := pieceLength / s.BlockLength
	if pieceLength%s.BlockLength != 0 {
		nBlocks++
	}
	piece := &Piece{
		SHA1:   torrent.Info.Pieces[index],
		Index:  index,
		blocks: make([]*Block, nBlocks),
		Done:   make(chan struct{}),
	}
	torrent.pieces[index] = piece
	return piece
}

func (s *Storage) GetBlock(infoHash [20]byte, index, begin, length int) ([]byte, error) {
	torrent := s.GetTorrent(infoHash)
	if torrent.Complete {
		return s.getBlockFromTorrent(torrent, index, begin, length)
	}
	piece := torrent.pieces[index]
	piece.mu.Lock()
	if piece == nil || !piece.Complete {
		panic(errors.New("GetBlock: incomplete piece"))
	}
	piece.mu.Unlock()
	f, err := os.Open(path.Join(s.WorkDir, torrent.WorkDir(), piece.Filename()))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data := make([]byte, length)
	n, err := f.ReadAt(data, int64(begin))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return data[:n], nil
}

func (s *Storage) getBlockFromTorrent(torrent *Torrent, index, begin, length int) ([]byte, error) {
	if len(torrent.Info.Files) > 0 {
		return s.getBlockFromMultiTorrent(torrent, index, begin, length)
	}
	filename := path.Join(s.TargetDir, torrent.Filename())
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data := make([]byte, length)
	offset := index*torrent.Info.PieceLength + begin
	n, err := f.ReadAt(data, int64(offset))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if n < len(data) {
		return nil, fmt.Errorf("incomplete read: %d < %d", n, len(data))
	}
	return data, nil
}

func (s *Storage) getBlockFromMultiTorrent(torrent *Torrent, index, begin, length int) ([]byte, error) {
	curr := 0
	data := make([]byte, length)
	total := 0
	start := index*torrent.Info.PieceLength + begin
	end := start + length
	for _, file := range torrent.Info.Files {
		if total > end {
			break
		}
		if total+file.Length >= start {
			offset := 0
			if total < start {
				offset = start - total
			}
			filepath := append([]string{s.TargetDir, torrent.Filename()}, file.Path...)
			f, err := os.Open(path.Join(filepath...))
			if err != nil {
				return nil, err
			}
			n, err := f.ReadAt(data[curr:], int64(offset))
			curr += n
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			f.Close()
		}
		total += file.Length
	}
	if curr < len(data) {
		return nil, fmt.Errorf("incomplete read: %d < %d", curr, len(data))
	}
	return data, nil
}

func (s *Storage) PutBlock(infoHash [20]byte, index, begin int, data []byte) error {
	t := s.GetTorrent(infoHash)
	if t.Complete {
		return nil
	}
	piece := s.GetPiece(t.Info.SHA1, index)
	piece.mu.Lock()
	defer piece.mu.Unlock()
	if piece.Complete {
		return nil
	}
	if piece.blocks[begin/s.BlockLength] != nil {
		return nil
	}
	block := &Block{
		Index: index,
		Begin: begin,
		Data:  data,
	}
	piece.blocks[block.Begin/s.BlockLength] = block
	piece.completeBlocks++
	if err := s.coalesceBlocksForPiece(t, piece); err != nil {
		return err
	}
	if err := s.coalescePiecesForTorrent(t); err != nil {
		return err
	}
	return nil
}

func (s *Storage) coalesceBlocksForPiece(torrent *Torrent, piece *Piece) error {
	if piece.completeBlocks != len(piece.blocks) {
		return nil
	}
	temp, err := os.CreateTemp(s.TempDir, piece.Filename())
	if err != nil {
		return err
	}
	defer temp.Close()
	sha1Digest := sha1.New()
	for _, b := range piece.blocks {
		r := io.TeeReader(bytes.NewReader(b.Data), sha1Digest)
		if _, err := io.Copy(temp, r); err != nil {
			return err
		}
	}
	var hash [20]byte
	sha1Digest.Sum(hash[:])
	if hash != piece.SHA1 {
		return fmt.Errorf("piece SHA1 does not match target: got %s != want %s",
			hex.EncodeToString(hash[:]),
			hex.EncodeToString(piece.SHA1[:]),
		)
	}
	if err = os.Rename(temp.Name(), path.Join(s.WorkDir, torrent.WorkDir(), piece.Filename())); err != nil {
		return err
	}
	piece.blocks = nil
	piece.Complete = true
	close(piece.Done)
	torrent.mu.Lock()
	defer torrent.mu.Unlock()
	torrent.Bitfield.Add(piece.Index)
	torrent.completePieces++
	return nil
}

func (s *Storage) coalescePiecesForTorrent(torrent *Torrent) error {
	if torrent.completePieces != len(torrent.pieces) {
		return nil
	}
	torrent.mu.Lock()
	defer torrent.mu.Unlock()
	if len(torrent.Info.Files) > 0 {
		return s.coalescePiecesForMultiTorrent(torrent)
	}
	temp, err := os.CreateTemp(s.TempDir, torrent.Filename())
	if err != nil {
		return err
	}
	defer temp.Close()
	var pieceReaders []io.Reader
	for _, piece := range torrent.pieces {
		path := path.Join(s.WorkDir, torrent.WorkDir(), piece.Filename())
		pieceReaders = append(pieceReaders, &lazyFileReader{path: path})
	}
	sha1Digest := sha1.New()
	mr := io.MultiReader(pieceReaders...)
	r := io.TeeReader(mr, sha1Digest)
	if _, err := io.Copy(temp, r); err != nil {
		return err
	}
	var hash [20]byte
	sha1Digest.Sum(hash[:])
	if hash != torrent.Info.SHA1 {
		return fmt.Errorf("torrent SHA1 does not match target: got %s != want %s",
			hex.EncodeToString(hash[:]),
			hex.EncodeToString(torrent.Info.SHA1[:]),
		)
	}
	if err = os.Rename(temp.Name(), path.Join(s.TargetDir, torrent.Filename())); err != nil {
		return err
	}
	if err := os.RemoveAll(path.Join(s.WorkDir, torrent.WorkDir())); err != nil {
		return err
	}
	torrent.Complete = true
	close(torrent.Done)
	return nil
}

func (s *Storage) coalescePiecesForMultiTorrent(torrent *Torrent) error {
	temp, err := os.MkdirTemp(s.TempDir, torrent.Filename())
	if err != nil {
		return err
	}
	var pieceReaders []io.Reader
	for _, piece := range torrent.pieces {
		path := path.Join(s.WorkDir, torrent.WorkDir(), piece.Filename())
		pieceReaders = append(pieceReaders, &lazyFileReader{path: path})
	}
	sha1Digest := sha1.New()
	mr := io.MultiReader(pieceReaders...)
	r := io.TeeReader(mr, sha1Digest)
	for _, file := range torrent.Info.Files {
		filepath := append([]string{temp}, file.Path...)
		if err := os.MkdirAll(path.Join(filepath[:len(filepath)-1]...), fs.FileMode(os.O_RDWR)); err != nil {
			return err
		}
		f, err := os.Create(path.Join(filepath...))
		if err != nil {
			return err
		}
		if _, err = io.CopyN(f, r, int64(file.Length)); err != nil {
			return err
		}
		f.Close()
	}
	var hash [20]byte
	sha1Digest.Sum(hash[:])
	if hash != torrent.Info.SHA1 {
		return fmt.Errorf("torrent SHA1 does not match target: got %s != want %s",
			hex.EncodeToString(hash[:]),
			hex.EncodeToString(torrent.Info.SHA1[:]),
		)
	}
	if err = os.Rename(temp, path.Join(s.TargetDir, torrent.Filename())); err != nil {
		return err
	}
	if err := os.RemoveAll(path.Join(s.WorkDir, torrent.WorkDir())); err != nil {
		return err
	}
	torrent.Complete = true
	close(torrent.Done)
	return nil
}
