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
	rbuf          [16]byte // for header
	streamFrameCh map[uint64]chan<- *Frame
	ctx           context.Context
}

// newFrameReader creates a FrameWriter instance to read frames
func newFrameReader(ctx context.Context, rwc net.Conn, timeout int) *defaultFrameReader {
	return &defaultFrameReader{Reader: NewReaderWithTimeout(ctx, rwc, timeout), ctx: ctx}
}

var (
	// ErrInvalidFrameSize when invalid size
	ErrInvalidFrameSize = errors.New("invalid frame size")
	// ErrStreamFrameMustNB when stream frame not non block
	ErrStreamFrameMustNB = errors.New("streaming frame must be non block")
)

func (dfr *defaultFrameReader) ReadFrame() (*Frame, error) {

	f, err := dfr.readFrame()
	if err != nil {
		return f, err
	}

	requestID := f.RequestID
	flags := f.Flags

	// done for non streamed frame
	if flags&StreamFlag == 0 {
		return f, nil
	}

	// deal with streamed frames

	if flags&NBFlag == 0 {
		return nil, ErrStreamFrameMustNB
	}

	if flags&StreamEndFlag == 0 {
		if dfr.streamFrameCh == nil {
			// the first stream for the connection
			dfr.streamFrameCh = make(map[uint64]chan<- *Frame)
		}
		ch, ok := dfr.streamFrameCh[requestID]
		if !ok {
			// the first frame for the stream
			ch = make(chan<- *Frame)
			dfr.streamFrameCh[requestID] = ch
			return f, nil
		}

		// continuation frame for the stream
		select {
		case ch <- f:
			return dfr.ReadFrame()
		case <-dfr.ctx.Done():
			return nil, dfr.ctx.Err()
		}
	} else {
		// the ending frame of the stream
		if dfr.streamFrameCh == nil {
			// ending frame with no prior stream frames
			return f, nil
		}
		ch, ok := dfr.streamFrameCh[requestID]
		if !ok {
			// ending frame with no prior stream frames
			return f, nil
		}
		// ending frame for the stream
		select {
		case ch <- f:
			delete(dfr.streamFrameCh, requestID)
			return dfr.ReadFrame()
		case <-dfr.ctx.Done():
			return nil, dfr.ctx.Err()
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
	flags := PacketFlag(cmdAndFlags >> 24)
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

func (dfr *defaultFrameReader) CloseStream(requestID uint64) {
	delete(dfr.streamFrameCh, requestID)
}
