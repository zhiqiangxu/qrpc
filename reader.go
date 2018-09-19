package qrpc

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
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
	// CtxCheckMaxInterval for check ctx.Done
	CtxCheckMaxInterval = 3 * time.Second
)

// NewReader creates a StreamReader instance
func NewReader(ctx context.Context, conn net.Conn) *Reader {
	return NewReaderWithTimeout(ctx, conn, ReadNoTimeout)
}

// NewReaderWithTimeout allows specify timeout
func NewReaderWithTimeout(ctx context.Context, conn net.Conn, timeout int) *Reader {
	if ctx == nil {
		ctx = context.Background()
	}

	return &Reader{ctx: ctx, conn: conn, reader: bufio.NewReader(conn), timeout: timeout}
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
		endTime     time.Time
		readTimeout time.Duration
		offset      int
		n           int
	)
	timeout := r.timeout
	if timeout > 0 {
		endTime = time.Now().Add(time.Duration(timeout) * time.Second)
	}
	size := len(bytes)

	for {

		if timeout > 0 {
			readTimeout = endTime.Sub(time.Now())
			if readTimeout > CtxCheckMaxInterval {
				readTimeout = CtxCheckMaxInterval
			}
		} else {
			readTimeout = CtxCheckMaxInterval
		}

		r.conn.SetReadDeadline(time.Now().Add(readTimeout))
		n, err = io.ReadFull(r.reader, bytes[offset:])
		offset += n
		if err != nil {
			if opError, ok := err.(*net.OpError); ok && opError.Timeout() {
				if timeout > 0 && time.Now().After(endTime) {
					return err
				}
			} else {
				return err
			}
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
