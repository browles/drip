package storage

import (
	"errors"
	"io"
	"os"
)

type lazyFileReader struct {
	path string
	f    *os.File
	read int64
	size int64
}

func (lfr *lazyFileReader) checkOpen() error {
	if lfr.f == nil {
		stat, err := os.Stat(lfr.path)
		if err != nil {
			return err
		}
		lfr.size = stat.Size()
		lfr.f, err = os.Open(lfr.path)
		if err != nil {
			return err
		}
	}
	return nil
}

func (lfr *lazyFileReader) checkClose() {
	if lfr.read == lfr.size {
		lfr.f.Close()
	}
}

func (lfr *lazyFileReader) Read(p []byte) (n int, err error) {
	defer func() {
		if errors.Is(err, io.EOF) {
			lfr.checkClose()
		}
	}()
	err = lfr.checkOpen()
	if err != nil {
		return 0, err
	}
	n, err = lfr.f.Read(p)
	lfr.read += int64(n)
	return
}

func (lfr *lazyFileReader) WriteTo(w io.Writer) (sum int64, err error) {
	defer lfr.checkClose()
	err = lfr.checkOpen()
	if err != nil {
		return 0, err
	}
	sum, err = lfr.f.WriteTo(w)
	lfr.read += sum
	return
}
