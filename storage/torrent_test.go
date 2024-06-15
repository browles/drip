package storage

import (
	"reflect"
	"testing"

	"github.com/browles/drip/api/metainfo"
)

func TestTorrent_createPiece(t *testing.T) {
	blockAligned := &Torrent{
		Info: &metainfo.Info{
			Length:      4 * 8 * BLOCK_LENGTH,
			PieceLength: 8 * BLOCK_LENGTH,
			Pieces:      [][20]byte{{1: 1}, {2: 2}, {3: 3}, {4: 4}},
		},
	}
	blockMisaligned := &Torrent{
		Info: &metainfo.Info{
			Length:      4*8*BLOCK_LENGTH - 1000,
			PieceLength: 8 * BLOCK_LENGTH,
			Pieces:      [][20]byte{{1: 1}, {2: 2}, {3: 3}, {4: 4}},
		},
	}
	blockAlignedShort := &Torrent{
		Info: &metainfo.Info{
			Length:      4*8*BLOCK_LENGTH - BLOCK_LENGTH,
			PieceLength: 8 * BLOCK_LENGTH,
			Pieces:      [][20]byte{{1: 1}, {2: 2}, {3: 3}, {4: 4}},
		},
	}
	pieceBlockAlignedShort := &Torrent{
		Info: &metainfo.Info{
			Length:      4 * 3 * BLOCK_LENGTH / 2,
			PieceLength: 3 * BLOCK_LENGTH / 2,
			Pieces:      [][20]byte{{1: 1}, {2: 2}, {3: 3}, {4: 4}},
		},
	}
	tests := []struct {
		name    string
		torrent *Torrent
		index   int
		want    *Piece
	}{
		{
			"block aligned", blockAligned, 0,
			&Piece{
				SHA1:      [20]byte{1: 1},
				Index:     0,
				numBlocks: 8,
			},
		},
		{
			"block aligned last", blockAligned, 3,
			&Piece{
				SHA1:      [20]byte{4: 4},
				Index:     3,
				numBlocks: 8,
			},
		},
		{
			"block misaligned", blockMisaligned, 0,
			&Piece{
				SHA1:      [20]byte{1: 1},
				Index:     0,
				numBlocks: 8,
			},
		},
		{
			"block misaligned last", blockMisaligned, 3,
			&Piece{
				SHA1:      [20]byte{4: 4},
				Index:     3,
				numBlocks: 8,
			},
		},
		{
			"block aligned short", blockAlignedShort, 0,
			&Piece{
				SHA1:      [20]byte{1: 1},
				Index:     0,
				numBlocks: 8,
			},
		},
		{
			"piece block misaligned", pieceBlockAlignedShort, 0,
			&Piece{
				SHA1:      [20]byte{1: 1},
				Index:     0,
				numBlocks: 2,
			},
		},
		{
			"pice block misaligned last", pieceBlockAlignedShort, 3,
			&Piece{
				SHA1:      [20]byte{4: 4},
				Index:     3,
				numBlocks: 2,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.torrent.createPiece(tt.index)
			got.Done = nil // cannot compare channels
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Torrent.createPiece() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
