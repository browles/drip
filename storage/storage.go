package storage

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/browles/drip/api/metainfo"
)

const BLOCK_LENGTH = 1 << 14

type Storage struct {
	TargetDir string
	WorkDir   string
	TempDir   string

	mu       sync.RWMutex
	torrents map[[20]byte]*Torrent
}

func New(targetDir, workDir, tempDir string) *Storage {
	return &Storage{
		TargetDir: targetDir,
		WorkDir:   workDir,
		TempDir:   tempDir,
		torrents:  make(map[[20]byte]*Torrent),
	}
}

func (s *Storage) AddTorrent(info *metainfo.Info) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.torrents[info.SHA1]; ok {
		return nil
	}
	torrent := newTorrent(info)
	_, err := os.Stat(filepath.Join(s.TargetDir, torrent.FileName()))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		// Complete torrent exists on disk
		for i := range len(torrent.Info.Pieces) {
			torrent.completePiece(torrent.pieces[i])
		}
		torrent.complete()
	} else {
		// Check for complete pieces on disk
		direntries, err := os.ReadDir(filepath.Join(s.WorkDir, torrent.WorkDir()))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			for _, dr := range direntries {
				var i int
				_, err := fmt.Sscanf(dr.Name(), "%d.piece", &i)
				if err != nil {
					continue
				}
				torrent.completePiece(torrent.pieces[i])
			}
			if err = s.coalescePieces(torrent); err != nil {
				return err
			}
		}
	}
	s.torrents[info.SHA1] = torrent
	return nil
}

func (s *Storage) GetTorrent(infohash [20]byte) *Torrent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	torrent, ok := s.torrents[infohash]
	if !ok {
		panic(errors.New("storage: GetTorrent: unknown info hash"))
	}
	return torrent
}

func (s *Storage) GetPiece(infoHash [20]byte, index int) *Piece {
	return s.GetTorrent(infoHash).pieces[index]
}

func (s *Storage) GetBlock(infoHash [20]byte, index, begin, length int) ([]byte, error) {
	torrent := s.GetTorrent(infoHash)
	torrent.mu.RLock()
	defer torrent.mu.RUnlock()
	if torrent.coalesced {
		return s.getBlockFromTorrent(torrent, index, begin, length)
	}
	piece := torrent.pieces[index]
	piece.mu.RLock()
	defer piece.mu.RUnlock()
	if !piece.coalesced {
		return nil, errors.New("storage: GetBlock: incomplete piece")
	}
	return s.getBlockFromPiece(torrent, piece, begin, length)
}

func (s *Storage) PutBlock(infoHash [20]byte, index, begin int, data []byte) error {
	torrent := s.GetTorrent(infoHash)
	if err := checkBlockSize(torrent.Info, index, begin, len(data)); err != nil {
		return err
	}
	torrent.mu.RLock()
	if torrent.coalesced {
		torrent.mu.RUnlock()
		return nil
	}
	torrent.mu.RUnlock()
	piece := torrent.pieces[index]
	piece.mu.Lock()
	defer piece.mu.Unlock()
	piece.putBlock(begin, data)
	torrent.mu.Lock()
	defer torrent.mu.Unlock()
	if err := s.coalesceBlocks(torrent, piece); err != nil {
		return err
	}
	if err := s.coalescePieces(torrent); err != nil {
		return err
	}
	return nil
}

type ChecksumError struct {
	Got  [20]byte
	Want [20]byte
}

func (cse *ChecksumError) Error() string {
	return fmt.Sprintf("storage: SHA1 does not match target: got %x != want %x", cse.Got, cse.Want)
}

func (s *Storage) getBlockFromTorrent(torrent *Torrent, index, begin, length int) ([]byte, error) {
	if len(torrent.Info.Files) > 0 {
		return s.getBlockFromMultiFile(torrent, index, begin, length)
	}
	return s.getBlockFromSingleFile(torrent, index, begin, length)
}

func (s *Storage) getBlockFromSingleFile(torrent *Torrent, index, begin, length int) ([]byte, error) {
	filename := filepath.Join(s.TargetDir, torrent.FileName())
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data := make([]byte, length)
	offset := index*torrent.Info.PieceLength + begin
	n, err := f.ReadAt(data, int64(offset))
	if err != nil {
		return nil, err
	}
	if n < len(data) {
		return nil, fmt.Errorf("storage: incomplete read: %d < %d", n, len(data))
	}
	return data, nil
}

func (s *Storage) getBlockFromMultiFile(torrent *Torrent, index, begin, length int) ([]byte, error) {
	curr := 0
	data := make([]byte, length)
	total := 0
	start := index*torrent.Info.PieceLength + begin
	end := start + length
	for _, file := range torrent.Info.Files {
		if total >= end {
			break
		}
		if total+file.Length >= start {
			offset := 0
			if total < start {
				offset = start - total
			}
			fp := append([]string{s.TargetDir, torrent.FileName()}, file.Path...)
			f, err := os.Open(filepath.Join(fp...))
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
		return nil, fmt.Errorf("storage: incomplete read: %d < %d", curr, len(data))
	}
	return data, nil
}

func (s *Storage) getBlockFromPiece(torrent *Torrent, piece *Piece, begin, length int) ([]byte, error) {
	f, err := os.Open(filepath.Join(s.WorkDir, torrent.WorkDir(), piece.FileName()))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data := make([]byte, length)
	n, err := f.ReadAt(data, int64(begin))
	if err != nil {
		return nil, err
	}
	return data[:n], nil
}

func checkBlockSize(info *metainfo.Info, index, begin, length int) error {
	if begin%BLOCK_LENGTH != 0 {
		return fmt.Errorf("storage: misaligned block: %d %% %d != 0", begin, BLOCK_LENGTH)
	}
	pieceLength := info.PieceLength
	if index == len(info.Pieces)-1 {
		pieceLength = info.Length - index*info.PieceLength
	}
	expectedBlockLength := BLOCK_LENGTH
	lastBlock := BLOCK_LENGTH * (pieceLength / BLOCK_LENGTH)
	if begin == lastBlock {
		expectedBlockLength = pieceLength - begin
	}
	if expectedBlockLength != length {
		return fmt.Errorf("storage: unexpected block length: %d != %d", length, expectedBlockLength)
	}
	return nil
}

func (s *Storage) coalesceBlocks(torrent *Torrent, piece *Piece) error {
	if piece.completeBlocks != len(piece.blocks) {
		return nil
	}
	temp, err := os.CreateTemp(s.TempDir, piece.FileName())
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	var blockReaders []io.Reader
	for _, b := range piece.blocks {
		blockReaders = append(blockReaders, bytes.NewReader(b.data))
	}
	mr := io.MultiReader(blockReaders...)
	sha1Digest := sha1.New()
	r := io.TeeReader(mr, sha1Digest)
	if _, err := io.Copy(temp, r); err != nil {
		return err
	}
	var hash [20]byte
	sha1Digest.Sum(hash[:0])
	if hash != piece.SHA1 {
		return &ChecksumError{hash, piece.SHA1}
	}
	if err = os.MkdirAll(filepath.Join(s.WorkDir, torrent.WorkDir()), 0o0700); err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), filepath.Join(s.WorkDir, torrent.WorkDir(), piece.FileName())); err != nil {
		return err
	}
	torrent.completePiece(piece)
	return nil
}

func (s *Storage) coalescePieces(torrent *Torrent) error {
	if torrent.completePieces != len(torrent.pieces) {
		return nil
	}
	if len(torrent.Info.Files) > 0 {
		return s.coalescePiecesForMultiFile(torrent)
	}
	return s.coalescePiecesForSingleFile(torrent)
}

func (s *Storage) coalescePiecesForSingleFile(torrent *Torrent) error {
	temp, err := os.CreateTemp(s.TempDir, torrent.FileName())
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	var pieceReaders []io.Reader
	for _, piece := range torrent.pieces {
		path := filepath.Join(s.WorkDir, torrent.WorkDir(), piece.FileName())
		pieceReaders = append(pieceReaders, &lazyFileReader{path: path})
	}
	r := io.MultiReader(pieceReaders...)
	if _, err := io.Copy(temp, r); err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), filepath.Join(s.TargetDir, torrent.FileName())); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.WorkDir, torrent.WorkDir())); err != nil {
		return err
	}
	torrent.complete()
	return nil
}

func (s *Storage) coalescePiecesForMultiFile(torrent *Torrent) error {
	temp, err := os.MkdirTemp(s.TempDir, torrent.FileName())
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	var pieceReaders []io.Reader
	for _, piece := range torrent.pieces {
		path := filepath.Join(s.WorkDir, torrent.WorkDir(), piece.FileName())
		pieceReaders = append(pieceReaders, &lazyFileReader{path: path})
	}
	r := io.MultiReader(pieceReaders...)
	for _, file := range torrent.Info.Files {
		fp := append([]string{temp}, file.Path...)
		if err := os.MkdirAll(filepath.Join(fp[:len(fp)-1]...), 0o0700); err != nil {
			return err
		}
		f, err := os.Create(filepath.Join(fp...))
		if err != nil {
			return err
		}
		if _, err = io.CopyN(f, r, int64(file.Length)); err != nil {
			return err
		}
		f.Close()
	}
	if err = os.Rename(temp, filepath.Join(s.TargetDir, torrent.FileName())); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.WorkDir, torrent.WorkDir())); err != nil {
		return err
	}
	torrent.complete()
	return nil
}
