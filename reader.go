package qrpc

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"
)

// Reader read data from socket
type Reader struct {
	conn    net.Conn
	reader  *bufio.Reader
	timeout int
	ctx     context.Context
}

const (
	// ReadNoTimeout will never timeout
	ReadNoTimeout = -1
	// ReadBufSize for read buf
	ReadBufSize = 1024
)

// NewReader creates a StreamReader instance
func NewReader(ctx context.Context, conn net.Conn) *Reader {
	return NewReaderWithTimeout(ctx, conn, ReadNoTimeout)
}

var bufPool = sync.Pool{New: func() interface{} {
	return bufio.NewReaderSize(nil, ReadBufSize)
}}

// NewReaderWithTimeout allows specify timeout
func NewReaderWithTimeout(ctx context.Context, conn net.Conn, timeout int) *Reader {
	if ctx == nil {
		ctx = context.Background()
	}

	bufReader := bufPool.Get().(*bufio.Reader)
	bufReader.Reset(conn)
	return &Reader{ctx: ctx, conn: conn, reader: bufReader, timeout: timeout}
}

// Finalize is called when no longer used
func (r *Reader) Finalize() {
	r.reader.Reset(nil)
	bufPool.Put(r.reader)
	r.reader = nil
}

// SetReadTimeout allows modify timeout for read
func (r *Reader) SetReadTimeout(timeout int) {
	r.timeout = timeout
}

// ReadUint32 read uint32 from socket
func (r *Reader) ReadUint32() (uint32, error) {
	bytes := make([]byte, 4)
	err := r.ReadBytes(bytes)
	if err != nil {
		return 0, err
	}

	return binary.BigEndian.Uint32(bytes), nil
}

// ReadBytes read bytes honouring CtxCheckMaxInterval
func (r *Reader) ReadBytes(bytes []byte) (err error) {
	var (
		endTime time.Time
		offset  int
		n       int
	)
	timeout := r.timeout
	if timeout > 0 {
		endTime = time.Now().Add(time.Duration(timeout) * time.Second)
	} else {
		endTime = time.Time{}
	}
	size := len(bytes)

	for {

		r.conn.SetReadDeadline(endTime)
		n, err = io.ReadFull(r.reader, bytes[offset:])
		offset += n
		if err != nil {
			return err
		}
		if offset >= size {
			return nil
		}

		select {
		case <-r.ctx.Done():
			return r.ctx.Err()
		default:
		}
	}

}
