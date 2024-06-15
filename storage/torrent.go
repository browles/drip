package storage

import (
	"encoding/hex"
	"fmt"
	"slices"
	"sync"

	"github.com/browles/drip/api/metainfo"
	"github.com/browles/drip/bitfield"
)

type Torrent struct {
	Info *metainfo.Info
	Done chan struct{}

	mu             sync.RWMutex
	bitfield       bitfield.Bitfield
	coalesced      bool
	completePieces int
	pieces         []*Piece
}

func (t *Torrent) WorkDir() string {
	return hex.EncodeToString(t.Info.SHA1[:])
}

func (t *Torrent) FileName() string {
	return t.Info.Name
}

func (t *Torrent) Bitfield() bitfield.Bitfield {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return slices.Clone(t.bitfield)
}

func (t *Torrent) GetPiece(index int) *Piece {
	return t.pieces[index]
}

type Piece struct {
	SHA1      [20]byte
	Done      chan struct{}
	Index     int
	numBlocks int

	mu             sync.RWMutex
	coalesced      bool
	completeBlocks int
	blocks         []*block
}

func (p *Piece) FileName() string {
	return fmt.Sprintf("%d.piece", p.Index)
}

type block struct {
	index int
	begin int
	data  []byte
}

func (t *Torrent) createPiece(index int) *Piece {
	pieceLength := t.Info.PieceLength
	if index == len(t.Info.Pieces)-1 {
		pieceLength = t.Info.Length - index*t.Info.PieceLength
	}
	numBlocks := pieceLength / BLOCK_LENGTH
	if pieceLength%BLOCK_LENGTH != 0 {
		numBlocks++
	}
	return &Piece{
		SHA1:      t.Info.Pieces[index],
		Index:     index,
		Done:      make(chan struct{}),
		numBlocks: numBlocks,
	}
}

func (t *Torrent) completePiece(piece *Piece) {
	piece.coalesced = true
	piece.blocks = nil
	close(piece.Done)
	t.bitfield.Add(piece.Index)
	t.completePieces++
}

func (t *Torrent) complete() {
	t.coalesced = true
	close(t.Done)
}
