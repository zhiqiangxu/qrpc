package qrpc

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
)

var (
	// ErrInvalidFrameSize when invalid size
	ErrInvalidFrameSize = errors.New("invalid frame size")
	// ErrFrameTooLarge when frame size too large
	ErrFrameTooLarge = errors.New("frame size too large")
)

// defaultFrameReader is responsible for read frames
// should create one instance per connection
type defaultFrameReader struct {
	*Reader
	rbuf         [16]byte // for header
	ctx          context.Context
	maxFrameSize int
}

// newFrameReader creates a FrameWriter instance to read frames
func newFrameReader(ctx context.Context, rwc net.Conn, timeout int) *defaultFrameReader {
	return newFrameReaderWithMFS(ctx, rwc, timeout, 0)
}

func newFrameReaderWithMFS(ctx context.Context, rwc net.Conn, timeout int, maxFrameSize int) *defaultFrameReader {
	return &defaultFrameReader{Reader: NewReaderWithTimeout(ctx, rwc, timeout), ctx: ctx, maxFrameSize: maxFrameSize}
}

// ReadFrame will only return the first frame in stream
func (dfr *defaultFrameReader) ReadFrame(cs *connstreams) (*Frame, error) {
start:
	f, err := dfr.readFrame()
	if err != nil {
		return f, err
	}

	requestID := f.RequestID
	flags := f.Flags

	// ReadFrame is not threadsafe, so below need not be atomic

	for {

		// handle Rst
		if flags.IsRst() {

			s := cs.GetStream(requestID, flags)
			if s != nil {
				s.ResetByPeer()
			}

			goto start
		}
		s := cs.CreateOrGetStream(dfr.ctx, requestID, flags)

		if s.TryBind(f) {
			return f, nil
		}
		ok := s.AddInFrame(f)
		if !ok {
			<-s.Done()
		}

		goto start
	}

}

func (dfr *defaultFrameReader) readFrame() (*Frame, error) {

	header := dfr.rbuf[:]
	err := dfr.ReadBytes(header)
	if err != nil {
		return nil, err
	}

	size := binary.BigEndian.Uint32(header)
	if dfr.maxFrameSize > 0 && size > uint32(dfr.maxFrameSize) {
		logError("ErrFrameTooLarge", size)
		return nil, ErrFrameTooLarge
	}
	requestID := binary.BigEndian.Uint64(header[4:])
	cmdAndFlags := binary.BigEndian.Uint32(header[12:])
	cmd := Cmd(cmdAndFlags & 0xffffff)
	flags := FrameFlag(cmdAndFlags >> 24)
	if size < 12 {
		return nil, ErrInvalidFrameSize
	}

	payload := make([]byte, size-12)
	err = dfr.ReadBytes(payload)
	if err != nil {
		return nil, err
	}

	return &Frame{RequestID: requestID, Cmd: cmd, Flags: flags, Payload: payload}, nil
}
