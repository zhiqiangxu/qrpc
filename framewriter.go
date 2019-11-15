package qrpc

import (
	"encoding/binary"
	"errors"
)

// frameBytesWriter for writing frame bytes
type frameBytesWriter interface {
	getCodec() Codec
	// writeFrameBytes write the frame bytes atomically or error
	writeFrameBytes(dfw *defaultFrameWriter) error
}

// defaultFrameWriter is responsible for write frames
// should create one instance per goroutine
type defaultFrameWriter struct {
	fbw  frameBytesWriter
	wbuf []byte
	resp *response
}

// newFrameWriter creates a FrameWriter instance to write frames
func newFrameWriter(fbw frameBytesWriter) *defaultFrameWriter {
	return &defaultFrameWriter{fbw: fbw}
}

// StartWrite Write the FrameHeader.
func (dfw *defaultFrameWriter) StartWrite(requestID uint64, cmd Cmd, flags FrameFlag) {

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

func (dfw *defaultFrameWriter) Cmd() Cmd {
	return Cmd(uint32(dfw.wbuf[13])<<16 | uint32(dfw.wbuf[14])<<8 | uint32(dfw.wbuf[15]))
}

func (dfw *defaultFrameWriter) SetCmd(cmd Cmd) {
	_ = append(dfw.wbuf[0:13], byte(cmd>>16), byte(cmd>>8), byte(cmd))
}

func (dfw *defaultFrameWriter) RequestID() uint64 {
	requestID := binary.BigEndian.Uint64(dfw.wbuf[4:])
	return requestID
}

func (dfw *defaultFrameWriter) SetRequestID(requestID uint64) {
	binary.BigEndian.PutUint64(dfw.wbuf[4:], requestID)
}

func (dfw *defaultFrameWriter) Flags() FrameFlag {
	return FrameFlag(dfw.wbuf[12])
}

func (dfw *defaultFrameWriter) SetFlags(flags FrameFlag) {
	_ = append(dfw.wbuf[:12], byte(flags))
}

func (dfw *defaultFrameWriter) GetWbuf() []byte {
	return dfw.wbuf
}

func (dfw *defaultFrameWriter) Payload() []byte {
	return dfw.wbuf[16:]
}

var (
	// ErrNoCodec when no codec available
	ErrNoCodec = errors.New("no codec available")
)

// EndWrite finishes write frame
func (dfw *defaultFrameWriter) EndWrite() (err error) {

	if dfw.Flags().IsCodec() {
		codec := dfw.fbw.getCodec()
		if codec == nil {
			err = ErrNoCodec
			return
		}
		var encodedPayload []byte
		encodedPayload, err = codec.Encode(dfw.Payload())
		if err != nil {
			return
		}
		dfw.wbuf = dfw.wbuf[:16]
		dfw.WriteBytes(encodedPayload)
	}

	length := len(dfw.wbuf) - 4
	_ = append(dfw.wbuf[:0],
		byte(length>>24),
		byte(length>>16),
		byte(length>>8),
		byte(length))

	err = dfw.fbw.writeFrameBytes(dfw)
	dfw.wbuf = dfw.wbuf[:16]

	return
}

func (dfw *defaultFrameWriter) Length() int {
	return int(binary.BigEndian.Uint32(dfw.wbuf))
}

func (dfw *defaultFrameWriter) StreamEndWrite(end bool) error {
	if end {
		dfw.SetFlags(dfw.Flags().ToEndStream())
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

type defaultStreamWriter defaultFrameWriter

func (dsw *defaultStreamWriter) StartWrite(cmd Cmd) {
	dfw := (*defaultFrameWriter)(dsw)
	dfw.SetCmd(cmd)
}

func (dsw *defaultStreamWriter) RequestID() uint64 {
	return (*defaultFrameWriter)(dsw).RequestID()
}

func (dsw *defaultStreamWriter) WriteBytes(v []byte) {
	(*defaultFrameWriter)(dsw).WriteBytes(v)
}

func (dsw *defaultStreamWriter) EndWrite(end bool) error {
	return (*defaultFrameWriter)(dsw).StreamEndWrite(end)
}
