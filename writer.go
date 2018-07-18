package qrpc

import (
	"net"
	"time"
)

// Writer writes data to connection
type Writer struct {
	conn    net.Conn
	timeout int
}

const (
	// WriteNoTimeout will never timeout
	WriteNoTimeout = -1
)

// NewWriter new instance
func NewWriter(conn net.Conn) *Writer {
	return &Writer{conn: conn, timeout: WriteNoTimeout}
}

// NewWriterWithTimeout new instance with timeout
func NewWriterWithTimeout(conn net.Conn, timeout int) *Writer {
	return &Writer{conn: conn, timeout: timeout}
}

// Write writes bytes
func (w *Writer) Write(bytes []byte) (int, error) {
	size := len(bytes)

	offset := 0

	if w.timeout > 0 {
		w.conn.SetWriteDeadline(time.Now().Add(time.Duration(w.timeout) * time.Second))
	}
	for {
		n, err := w.conn.Write(bytes[offset:])
		offset += n
		if err != nil {
			return offset, err
		}
		if offset >= size {
			return offset, nil
		}
	}
}
