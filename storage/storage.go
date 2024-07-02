package storage

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/browles/drip/api/metainfo"
)

const BLOCK_LENGTH = 1 << 14

type Storage struct {
	TargetDir  string
	WorkDir    string
	Torrent    *Torrent
	downloaded atomic.Int64
}

var (
	ErrIncompletePiece = errors.New("storage: incomplete piece")
	ErrBlockExists     = errors.New("storage: block exists")
	ErrUnknownTorrent  = errors.New("storage: unknown info hash")
)

func New(targetDir, workDir string, info *metainfo.Info) *Storage {
	return &Storage{
		TargetDir: targetDir,
		WorkDir:   workDir,
		Torrent:   newTorrent(info),
	}
}

func (s *Storage) Load() error {
	st, err := os.Stat(filepath.Join(s.TargetDir, s.Torrent.FileName()))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		// Complete torrent exists on disk
		for i := range len(s.Torrent.Info.Pieces) {
			s.Torrent.completePiece(s.Torrent.pieces[i])
		}
		s.Torrent.complete()
		s.downloaded.Add(st.Size())
	} else {
		// Check for complete pieces on disk
		direntries, err := os.ReadDir(filepath.Join(s.WorkDir, s.Torrent.WorkDir()))
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
				st, err := os.Stat(dr.Name())
				if err != nil {
					return err
				}
				s.downloaded.Add(st.Size())
				s.Torrent.completePiece(s.Torrent.pieces[i])
			}
			if err = s.coalescePieces(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Storage) Downloaded() int64 {
	return s.downloaded.Load()
}

func (s *Storage) GetPiece(index int) *Piece {
	return s.Torrent.pieces[index]
}

func (s *Storage) ResetPiece(index int) (int64, *Piece) {
	piece := s.Torrent.pieces[index]
	piece.mu.Lock()
	defer piece.mu.Unlock()
	deleted := int64(0)
	for _, b := range piece.blocks {
		deleted += int64(len(b.data))
	}
	s.downloaded.Add(-deleted)
	piece.reset()
	return deleted, piece
}

func (s *Storage) GetBlock(index, begin, length int) ([]byte, error) {
	slog.Debug("GetBlock", "name", s.Torrent.Info.Name, "index", index, "begin", begin, "length", length)
	s.Torrent.mu.RLock()
	defer s.Torrent.mu.RUnlock()
	if s.Torrent.coalesced {
		return s.getBlockFromTorrent(index, begin, length)
	}
	piece := s.Torrent.pieces[index]
	piece.mu.RLock()
	defer piece.mu.RUnlock()
	if !piece.coalesced {
		return nil, ErrIncompletePiece
	}
	return s.getBlockFromPiece(piece, begin, length)
}

func (s *Storage) PutBlock(index, begin int, data []byte) error {
	slog.Debug("PutBlock", "name", s.Torrent.Info.Name, "index", index, "begin", begin, "length", len(data), "data", "<omitted>")
	if err := checkBlockSize(s.Torrent.Info, index, begin, len(data)); err != nil {
		return err
	}
	s.Torrent.mu.RLock()
	if s.Torrent.coalesced {
		s.Torrent.mu.RUnlock()
		return ErrBlockExists
	}
	s.Torrent.mu.RUnlock()
	piece := s.Torrent.pieces[index]
	piece.mu.Lock()
	defer piece.mu.Unlock()
	if err := piece.putBlock(begin, data); err != nil {
		return err
	}
	s.Torrent.mu.Lock()
	defer s.Torrent.mu.Unlock()
	s.downloaded.Add(int64(len(data)))
	if err := s.coalesceBlocks(piece); err != nil {
		piece.err.Deliver(err)
		return err
	}
	if err := s.coalescePieces(); err != nil {
		return err
	}
	return nil
}

type ChecksumError struct {
	Index int
	Got   [20]byte
	Want  [20]byte
}

func (cse *ChecksumError) Error() string {
	return fmt.Sprintf("storage: SHA1 does not match target, piece=%d: got %x != want %x", cse.Index, cse.Got, cse.Want)
}

func (s *Storage) getBlockFromTorrent(index, begin, length int) ([]byte, error) {
	if len(s.Torrent.Info.Files) > 0 {
		return s.getBlockFromMultiFile(index, begin, length)
	}
	return s.getBlockFromSingleFile(index, begin, length)
}

func (s *Storage) getBlockFromSingleFile(index, begin, length int) ([]byte, error) {
	filename := filepath.Join(s.TargetDir, s.Torrent.FileName())
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data := make([]byte, length)
	offset := index*s.Torrent.Info.PieceLength + begin
	n, err := f.ReadAt(data, int64(offset))
	if err != nil {
		return nil, err
	}
	if n < len(data) {
		return nil, fmt.Errorf("storage: incomplete read: %d < %d", n, len(data))
	}
	return data, nil
}

func (s *Storage) getBlockFromMultiFile(index, begin, length int) ([]byte, error) {
	curr := 0
	data := make([]byte, length)
	var total int64
	start := index*s.Torrent.Info.PieceLength + begin
	end := start + length
	for _, file := range s.Torrent.Info.Files {
		if total >= int64(end) {
			break
		}
		if total+file.Length >= int64(start) {
			offset := 0
			if total < int64(start) {
				offset = start - int(total)
			}
			fp := append([]string{s.TargetDir, s.Torrent.FileName()}, file.Path...)
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

func (s *Storage) getBlockFromPiece(piece *Piece, begin, length int) ([]byte, error) {
	f, err := os.Open(filepath.Join(s.WorkDir, s.Torrent.WorkDir(), piece.FileName()))
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
	pieceLength := info.GetPieceLength(index)
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

func (s *Storage) coalesceBlocks(piece *Piece) error {
	if piece.completeBlocks != len(piece.blocks) {
		return nil
	}
	slog.Debug("coalesceBlocks", "name", s.Torrent.Info.Name, "piece", piece.Index)
	tempDir := filepath.Join(s.WorkDir, "temp")
	if err := os.MkdirAll(tempDir, 0o0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(tempDir, s.Torrent.WorkDir()+"-"+piece.FileName())
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
		return &ChecksumError{piece.Index, hash, piece.SHA1}
	}
	if err = os.MkdirAll(filepath.Join(s.WorkDir, s.Torrent.WorkDir()), 0o0700); err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), filepath.Join(s.WorkDir, s.Torrent.WorkDir(), piece.FileName())); err != nil {
		return err
	}
	s.Torrent.completePiece(piece)
	return nil
}

func (s *Storage) coalescePieces() error {
	if s.Torrent.completePieces != len(s.Torrent.pieces) {
		return nil
	}
	slog.Debug("coalescePieces", "name", s.Torrent.Info.Name)
	if len(s.Torrent.Info.Files) > 0 {
		return s.coalescePiecesForMultiFile()
	}
	return s.coalescePiecesForSingleFile()
}

func (s *Storage) coalescePiecesForSingleFile() error {
	tempDir := filepath.Join(s.WorkDir, "temp")
	if err := os.MkdirAll(tempDir, 0o0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(tempDir, s.Torrent.FileName())
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	var pieceReaders []io.Reader
	for _, piece := range s.Torrent.pieces {
		path := filepath.Join(s.WorkDir, s.Torrent.WorkDir(), piece.FileName())
		pieceReaders = append(pieceReaders, &lazyFileReader{path: path})
	}
	r := io.MultiReader(pieceReaders...)
	if _, err := io.Copy(temp, r); err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), filepath.Join(s.TargetDir, s.Torrent.FileName())); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.WorkDir, s.Torrent.WorkDir())); err != nil {
		return err
	}
	s.Torrent.complete()
	return nil
}

func (s *Storage) coalescePiecesForMultiFile() error {
	tempDir := filepath.Join(s.WorkDir, "temp")
	if err := os.MkdirAll(tempDir, 0o0700); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(tempDir, s.Torrent.FileName())
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	var pieceReaders []io.Reader
	for _, piece := range s.Torrent.pieces {
		path := filepath.Join(s.WorkDir, s.Torrent.WorkDir(), piece.FileName())
		pieceReaders = append(pieceReaders, &lazyFileReader{path: path})
	}
	r := io.MultiReader(pieceReaders...)
	for _, file := range s.Torrent.Info.Files {
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
	if err = os.Rename(temp, filepath.Join(s.TargetDir, s.Torrent.FileName())); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.WorkDir, s.Torrent.WorkDir())); err != nil {
		return err
	}
	s.Torrent.complete()
	return nil
}
