package storage

import (
	"errors"
	"fmt"
	"sync"

	"github.com/browles/drip/api/metainfo"
	"github.com/browles/drip/bitfield"
	"github.com/browles/drip/future"
)

type Torrent struct {
	Info     *metainfo.Info
	Bitfield bitfield.Bitfield

	mu             sync.RWMutex
	coalesced      bool
	completePieces int
	pieces         []*Piece
	err            *future.Future[error]
}

func newTorrent(info *metainfo.Info) *Torrent {
	torrent := &Torrent{
		Info:   info,
		pieces: make([]*Piece, len(info.Pieces)),
		err:    future.New[error](),
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

func (t *Torrent) GetPiece(index int) *Piece {
	return t.pieces[index]
}

func (t *Torrent) Wait() error {
	t.mu.Lock()
	fut := t.err
	t.mu.Unlock()
	return fut.Wait()
}

func (t *Torrent) completePiece(piece *Piece) {
	piece.coalesced = true
	piece.err.Deliver(nil)
	piece.blocks = nil
	t.Bitfield.Add(piece.Index)
	t.completePieces++
}

func (t *Torrent) complete() {
	t.coalesced = true
	t.err.Deliver(nil)
}

type Piece struct {
	SHA1      [20]byte
	Index     int
	numBlocks int

	mu             sync.RWMutex
	coalesced      bool
	completeBlocks int
	blocks         []*block
	err            *future.Future[error]
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
		err:       future.New[error](),
	}
}

func (p *Piece) FileName() string {
	return fmt.Sprintf("%d.piece", p.Index)
}

func (p *Piece) Wait() error {
	p.mu.RLock()
	fut := p.err
	p.mu.RUnlock()
	return fut.Wait()
}

func (p *Piece) reset() {
	p.err.Deliver(errors.New("storage: piece reset"))
	p.err = future.New[error]()
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
