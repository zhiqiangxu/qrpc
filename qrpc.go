package qrpc

// Cmd is for multiplexer
type Cmd uint32

// PacketFlag defines flags for qrpc
type PacketFlag uint8

const (
	// StreamFlag means packet is streamed
	StreamFlag PacketFlag = 1 << iota
	// StreamEndFlag denotes the end of a stream
	StreamEndFlag
	// NBFlag means it should be handled nonblockly
	NBFlag
	// CancelFlag cancels a stream (TODO)
	CancelFlag
	// CompressFlag indicate packet is compressed (TODO)
	CompressFlag
	// PushFlag mean the frame is pushed from server
	PushFlag
)

// contextKey is a value for use with context.WithValue. It's used as
// a pointer so it fits in an interface{} without allocation.
type contextKey struct {
	name string
}

func (k *contextKey) String() string { return "qrpc context value " + k.name }
