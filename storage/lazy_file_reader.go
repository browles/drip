package storage

import (
	"errors"
	"io"
	"os"
)

type lazyFileReader struct {
	path string
	f    *os.File
}

func (lfr *lazyFileReader) Read(p []byte) (n int, err error) {
	if lfr.f == nil {
		lfr.f, err = os.Open(lfr.path)
		if err != nil {
			return 0, err
		}
	}
	n, err = lfr.f.Read(p)
	if err != nil && errors.Is(err, io.EOF) {
		lfr.f.Close()
	}
	return
}
