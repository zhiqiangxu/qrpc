package qrpc

import (
	"context"
	"net"
	"time"
)

// Writer writes data to connection
type Writer struct {
	ctx     context.Context
	conn    net.Conn
	timeout int
}

const (
	// WriteNoTimeout will never timeout
	WriteNoTimeout = -1
	// CtxCheckMaxInterval for check ctx.Done
	CtxCheckMaxInterval = 3 * time.Second
)

// NewWriter new instance
func NewWriter(ctx context.Context, conn net.Conn) *Writer {
	return &Writer{ctx: ctx, conn: conn, timeout: WriteNoTimeout}
}

// NewWriterWithTimeout new instance with timeout
func NewWriterWithTimeout(ctx context.Context, conn net.Conn, timeout int) *Writer {
	return &Writer{ctx: ctx, conn: conn, timeout: timeout}
}

// Write writes bytes
func (w *Writer) Write(bytes []byte) (int, error) {
	var (
		endTime      time.Time
		writeTimeout time.Duration
		offset       int
		n            int
		err          error
	)

	timeout := w.timeout
	if timeout > 0 {
		endTime = time.Now().Add(time.Duration(timeout) * time.Second)
	}
	size := len(bytes)

	for {
		if timeout > 0 {
			writeTimeout = endTime.Sub(time.Now())
			if writeTimeout > CtxCheckMaxInterval {
				writeTimeout = CtxCheckMaxInterval
			}
		} else {
			writeTimeout = CtxCheckMaxInterval
		}

		w.conn.SetWriteDeadline(time.Now().Add(writeTimeout))

		n, err = w.conn.Write(bytes[offset:])
		offset += n
		if err != nil {
			if opError, ok := err.(*net.OpError); ok && opError.Timeout() {
				if timeout > 0 && time.Now().After(endTime) {
					return offset, err
				}
			} else {
				return offset, err
			}
		}
		if offset >= size {
			return offset, nil
		}

		select {
		case <-w.ctx.Done():
			return offset, w.ctx.Err()
		default:
		}
	}

}

func (w *Writer) writeBuffers(buffs *net.Buffers) (int64, error) {
	var (
		endTime      time.Time
		writeTimeout time.Duration
		offset       int64
		n            int64
		err          error
	)

	timeout := w.timeout
	if timeout > 0 {
		endTime = time.Now().Add(time.Duration(timeout) * time.Second)
	}
	var size int64
	for _, bytes := range *buffs {
		size += int64(len(bytes))
	}

	for {
		if timeout > 0 {
			writeTimeout = endTime.Sub(time.Now())
			if writeTimeout > CtxCheckMaxInterval {
				writeTimeout = CtxCheckMaxInterval
			}
		} else {
			writeTimeout = CtxCheckMaxInterval
		}

		w.conn.SetWriteDeadline(time.Now().Add(writeTimeout))

		n, err = buffs.WriteTo(w.conn)
		offset += n
		if err != nil {
			if opError, ok := err.(*net.OpError); ok && opError.Timeout() {
				if timeout > 0 && time.Now().After(endTime) {
					return offset, err
				}
			} else {
				return offset, err
			}
		}
		if offset >= size {
			return offset, nil
		}

		select {
		case <-w.ctx.Done():
			return offset, w.ctx.Err()
		default:
		}
	}
}
