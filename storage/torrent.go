package storage

import (
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
	err            error
}

func (t *Torrent) WorkDir() string {
	return fmt.Sprintf("%x", t.Info.SHA1)
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

func (t *Torrent) Err() error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.err
}

type Piece struct {
	SHA1      [20]byte
	Index     int
	numBlocks int

	mu             sync.RWMutex
	Done           chan struct{}
	coalesced      bool
	completeBlocks int
	blocks         []*block
	err            error
}

func (p *Piece) FileName() string {
	return fmt.Sprintf("%d.piece", p.Index)
}

func (p *Piece) Err() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.err
}

func (p *Piece) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reset()
}

type block struct {
	index int
	begin int
	data  []byte
}

func newTorrent(info *metainfo.Info) *Torrent {
	torrent := &Torrent{
		Info:   info,
		Done:   make(chan struct{}),
		pieces: make([]*Piece, len(info.Pieces)),
	}
	for i := range len(info.Pieces) {
		torrent.pieces[i] = newPiece(info, i)
	}
	return torrent
}

func newPiece(info *metainfo.Info, index int) *Piece {
	pieceLength := info.GetPieceLength(index)
	numBlocks := pieceLength / BLOCK_LENGTH
	if pieceLength%BLOCK_LENGTH != 0 {
		numBlocks++
	}
	return &Piece{
		SHA1:      info.Pieces[index],
		Index:     index,
		Done:      make(chan struct{}),
		numBlocks: numBlocks,
	}
}

func (t *Torrent) completePiece(piece *Piece) {
	piece.coalesced = true
	close(piece.Done)
	piece.blocks = nil
	t.bitfield.Add(piece.Index)
	t.completePieces++
}

func (t *Torrent) complete() {
	t.coalesced = true
	close(t.Done)
}

func (p *Piece) putBlock(begin int, data []byte) error {
	if p.coalesced {
		return ErrBlockExists
	}
	if p.blocks == nil {
		p.blocks = make([]*block, p.numBlocks)
	}
	if p.blocks[begin/BLOCK_LENGTH] != nil {
		return ErrBlockExists
	}
	block := &block{
		index: p.Index,
		begin: begin,
		data:  data,
	}
	p.blocks[block.begin/BLOCK_LENGTH] = block
	p.completeBlocks++
	return nil
}

func (p *Piece) fail(err error) {
	p.err = err
	close(p.Done)
}

func (p *Piece) reset() {
	if !p.coalesced {
		p.Done = make(chan struct{})
	}
	p.err = nil
	p.blocks = nil
	p.completeBlocks = 0
}
