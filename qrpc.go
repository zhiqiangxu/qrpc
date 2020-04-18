package qrpc

// Cmd is for multiplexer
// only the lower two bytes are used for routing
// the 3rd byte can be used to store opaque value
type Cmd uint32

const (
	// MaxCmd for qrpc
	MaxCmd = 0xffffff
)

// Opaque returns the 3rd byte of Cmd
func (c Cmd) Opaque() uint8 {
	return uint8(c >> 16)
}

// Routing returns the lower two bytes
//go:nosplit
func (c Cmd) Routing() Cmd {
	return Cmd(c & 0xffff)
}

// CmdWithOpaque for combine Cmd and opaque
func CmdWithOpaque(c Cmd, opaque uint8) Cmd {
	return Cmd(uint32(c)&0xffff + uint32(opaque)<<16)
}

// FrameFlag defines type for qrpc frame
type FrameFlag uint8

const (
	// StreamFlag means packet is streamed
	StreamFlag FrameFlag = 1 << iota
	// StreamEndFlag denotes the end of a stream
	StreamEndFlag
	// StreamRstFlag is sent to request cancellation of a stream or to indicate that an error condition has occurred
	StreamRstFlag
	// NBFlag means it should be handled nonblockingly, it's implied for streamed frames
	NBFlag
	// PushFlag mean the frame is pushed from server
	PushFlag
	// CodecFlag for codec
	CodecFlag
)

// ToNonStream convert flg to nonstreamed flag
func (flg FrameFlag) ToNonStream() FrameFlag {
	return flg & ^(StreamFlag & StreamEndFlag)
}

// ToStream convert flg to streamed flag
func (flg FrameFlag) ToStream() FrameFlag {
	return flg | StreamFlag
}

// ToEndStream set StreamEndFlag on
func (flg FrameFlag) ToEndStream() FrameFlag {
	return flg | StreamEndFlag
}

// IsNonBlock means whether the frame should be processed nonblockingly
func (flg FrameFlag) IsNonBlock() bool {
	return flg.IsStream() || flg&NBFlag != 0
}

// IsRst returns whether flg is rst
func (flg FrameFlag) IsRst() bool {
	return flg&StreamRstFlag != 0
}

// IsStream means whether the frame is streamed
func (flg FrameFlag) IsStream() bool {
	return flg&StreamFlag != 0 || flg&StreamEndFlag != 0
}

// IsDone returns whether more continuation frame is expected or not
// true if:
// 1. not streamed
// 2. stream end flag set
// 3. stream rst flag set
func (flg FrameFlag) IsDone() bool {
	return !flg.IsStream() || flg&StreamEndFlag != 0 || flg&StreamRstFlag != 0
}

// IsPush checks whether frame is pushed
func (flg FrameFlag) IsPush() bool {
	return flg&PushFlag != 0
}

// IsCodec checks whether frame needs codec
func (flg FrameFlag) IsCodec() bool {
	return flg&CodecFlag != 0
}

// ToNonCodec convert flg to noncodec flag
func (flg FrameFlag) ToNonCodec() FrameFlag {
	return flg & ^CodecFlag
}

// ToCodec convert flg to codec flag
func (flg FrameFlag) ToCodec() FrameFlag {
	return flg | CodecFlag
}

// contextKey is a value for use with context.WithValue. It's used as
// a pointer so it fits in an interface{} without allocation.
type contextKey struct {
	name string
}

func (k *contextKey) String() string { return "qrpc context value " + k.name }
