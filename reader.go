package qrpc

import (
	"encoding/binary"
	"io"
	"net"
	"time"
)

// Reader read data from socket
type Reader struct {
	conn    net.Conn
	timeout int
}

const (
	// ReadNoTimeout will never timeout
	ReadNoTimeout = -1
)

// NewReader creates a StreamReader instance
func NewReader(conn net.Conn) *Reader {
	return NewReaderWithTimeout(conn, ReadNoTimeout)
}

// NewReaderWithTimeout allows specify timeout
func NewReaderWithTimeout(conn net.Conn, timeout int) *Reader {
	return &Reader{conn: conn, timeout: timeout}
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

// ReadBytes read bytes
func (r *Reader) ReadBytes(bytes []byte) error {
	timeout := r.timeout
	if timeout > 0 {
		r.conn.SetReadDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
	}
	_, err := io.ReadFull(r.conn, bytes)
	if err != nil {
		return err
	}

	return nil
}
