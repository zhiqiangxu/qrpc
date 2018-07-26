package qrpc

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
)

// defaultFrameReader is responsible for read frames
// should create one instance per connection
type defaultFrameReader struct {
	*Reader
	rbuf [16]byte // for header
	ctx  context.Context
}

// newFrameReader creates a FrameWriter instance to read frames
func newFrameReader(ctx context.Context, rwc net.Conn, timeout int) *defaultFrameReader {
	return &defaultFrameReader{Reader: NewReaderWithTimeout(ctx, rwc, timeout), ctx: ctx}
}

var (
	// ErrInvalidFrameSize when invalid size
	ErrInvalidFrameSize = errors.New("invalid frame size")
)

func (dfr *defaultFrameReader) ReadFrame(cs *connstreams) (*Frame, error) {

	f, err := dfr.readFrame()
	if err != nil {
		return f, err
	}

	requestID := f.RequestID
	flags := f.Flags

	// ReadFrame is not threadsafe, so below need not be atomic

	for {
		s := cs.CreateOrGetStream(dfr.ctx, requestID, f.Flags)

		if s.TryBind(f) {
			return f, nil
		}
		ok := s.AddInFrame(f)
		if !ok {
			<-s.Done()
			cs.DeleteStream(s, flags&PushFlag != 0)
		}
	}

}

func (dfr *defaultFrameReader) readFrame() (*Frame, error) {

	header := dfr.rbuf[:]
	err := dfr.ReadBytes(header)
	if err != nil {
		return nil, err
	}

	size := binary.BigEndian.Uint32(header)
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

	return &Frame{RequestID: requestID, Cmd: cmd, Flags: flags, Payload: payload, ctx: dfr.ctx}, nil
}
