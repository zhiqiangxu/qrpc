package qrpc

import (
	"context"
)

// defaultFrameWriter is responsible for write frames
// should create one instance per goroutine
type defaultFrameWriter struct {
	writeCh   chan<- writeFrameRequest
	wbuf      []byte
	requestID uint64
	cmd       Cmd
	flags     FrameFlag
	ctx       context.Context
}

// newFrameWriter creates a FrameWriter instance to write frames
func newFrameWriter(ctx context.Context, writeCh chan<- writeFrameRequest) *defaultFrameWriter {
	return &defaultFrameWriter{writeCh: writeCh, ctx: ctx}
}

// StartWrite Write the FrameHeader.
func (dfw *defaultFrameWriter) StartWrite(requestID uint64, cmd Cmd, flags FrameFlag) {

	dfw.requestID = requestID
	dfw.cmd = cmd
	dfw.flags = flags
	dfw.wbuf = append(dfw.wbuf[:0],
		0, // 4 bytes of length, filled in in endWrite
		0,
		0,
		0,
		byte(requestID>>56),
		byte(requestID>>48),
		byte(requestID>>40),
		byte(requestID>>32),
		byte(requestID>>24),
		byte(requestID>>16),
		byte(requestID>>8),
		byte(requestID),
		byte(flags),
		byte(cmd>>16),
		byte(cmd>>8),
		byte(cmd))

}

func (dfw *defaultFrameWriter) RequestID() uint64 {
	return dfw.requestID
}

func (dfw *defaultFrameWriter) Flags() FrameFlag {
	return dfw.flags
}

func (dfw *defaultFrameWriter) GetWbuf() []byte {
	return dfw.wbuf
}

// EndWrite finishes write frame
func (dfw *defaultFrameWriter) EndWrite() error {

	length := len(dfw.wbuf) - 4
	_ = append(dfw.wbuf[:0],
		byte(length>>24),
		byte(length>>16),
		byte(length>>8),
		byte(length))
	_ = append(dfw.wbuf[:12], byte(dfw.flags)) // flags may be changed by StreamWriter

	wfr := writeFrameRequest{dfw: dfw, result: make(chan error, 1)}
	select {
	case dfw.writeCh <- wfr:
	case <-dfw.ctx.Done():
		return dfw.ctx.Err()
	}

	select {
	case err := <-wfr.result:
		return err
	case <-dfw.ctx.Done():
		return dfw.ctx.Err()
	}
}

func (dfw *defaultFrameWriter) StreamEndWrite(end bool) error {
	if end {
		dfw.flags = dfw.flags.ToEndStream()
	}
	return dfw.EndWrite()
}

func (dfw *defaultFrameWriter) ResetFrame(requestID uint64, reason Cmd) error {
	dfw.StartWrite(requestID, reason, StreamRstFlag)
	return dfw.EndWrite()
}

// WriteUint64 write uint64 to wbuf
func (dfw *defaultFrameWriter) WriteUint64(v uint64) {
	dfw.wbuf = append(dfw.wbuf, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// WriteUint32 write uint32 to wbuf
func (dfw *defaultFrameWriter) WriteUint32(v uint32) {
	dfw.wbuf = append(dfw.wbuf, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// WriteUint16 write uint16 to wbuf
func (dfw *defaultFrameWriter) WriteUint16(v uint16) {
	dfw.wbuf = append(dfw.wbuf, byte(v>>8), byte(v))
}

// WriteUint8 write uint8 to wbuf
func (dfw *defaultFrameWriter) WriteUint8(v uint8) {
	dfw.wbuf = append(dfw.wbuf, byte(v))
}

// WriteBytes write multiple bytes
func (dfw *defaultFrameWriter) WriteBytes(v []byte) { dfw.wbuf = append(dfw.wbuf, v...) }
