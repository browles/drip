package storage

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/browles/drip/api/metainfo"
	"github.com/browles/drip/bitfield"
)

type Torrent struct {
	Info *metainfo.Info
	done *future

	mu             sync.RWMutex
	bitfield       bitfield.Bitfield
	coalesced      bool
	completePieces int
	pieces         []*Piece
}

func newTorrent(info *metainfo.Info) *Torrent {
	torrent := &Torrent{
		Info:   info,
		done:   newFuture(),
		pieces: make([]*Piece, len(info.Pieces)),
	}
	for i := range len(info.Pieces) {
		torrent.pieces[i] = newPiece(info, i)
	}
	return torrent
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

func (t *Torrent) Wait() error {
	_, err := t.done.wait()
	return err
}

func (t *Torrent) completePiece(piece *Piece) {
	piece.coalesced = true
	piece.done.finalize(nil, nil)
	piece.blocks = nil
	t.bitfield.Add(piece.Index)
	t.completePieces++
}

func (t *Torrent) complete() {
	t.coalesced = true
	t.done.finalize(nil, nil)
}

type Piece struct {
	SHA1      [20]byte
	Index     int
	numBlocks int

	mu             sync.RWMutex
	coalesced      bool
	completeBlocks int
	blocks         []*block
	done           *future
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
		numBlocks: numBlocks,
		done:      newFuture(),
	}
}

func (p *Piece) FileName() string {
	return fmt.Sprintf("%d.piece", p.Index)
}

func (p *Piece) Wait() error {
	p.mu.RLock()
	f := p.done
	p.mu.RUnlock()
	_, err := f.wait()
	return err
}

func (p *Piece) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done.finalize(nil, errors.New("storage: piece reset"))
	p.done = newFuture()
	p.blocks = nil
	p.completeBlocks = 0
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

type block struct {
	index int
	begin int
	data  []byte
}

type future struct {
	c   chan struct{}
	res any
	err error
}

func newFuture() *future {
	return &future{c: make(chan struct{})}
}

func (p *future) finalize(res any, err error) {
	p.res, p.err = res, err
	close(p.c)
}

func (p *future) wait() (any, error) {
	<-p.c
	return p.res, p.err
}
