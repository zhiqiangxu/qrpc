package qrpc

import (
	"context"
)

// Frame models a qrpc frame
// all fields are readly only
type Frame struct {
	RequestID uint64
	Flags     FrameFlag
	Cmd       Cmd
	Payload   []byte
	Stream    *stream // non nil for the first frame in stream
}

// FrameCh get the next frame ch
func (r *Frame) FrameCh() <-chan *Frame {
	return r.Stream.frameCh
}

// Context returns the request's context.
//
// The returned context is always non-nil;
//
// For outgoing client requests, the context controls cancelation.
//
// For incoming server requests, the context is canceled when the
// client's connection closes, the request is canceled ,
// or when the ServeQRPC method returns. (TODO)
func (r *Frame) Context() context.Context {
	return r.Stream.ctx
}

// RequestFrame is client->server
type RequestFrame Frame

// ConnectionInfo returns the underlying ConnectionInfo
func (r *RequestFrame) ConnectionInfo() *ConnectionInfo {

	return r.Stream.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)

}

// Close the underlying connection
func (r *RequestFrame) Close() error {

	ci := r.Stream.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)
	return ci.SC.Close()
}

// Context for RequestFrame
func (r *RequestFrame) Context() context.Context {
	return (*Frame)(r).Context()
}

// FrameCh for RequestFrame
func (r *RequestFrame) FrameCh() <-chan *Frame {
	return (*Frame)(r).FrameCh()
}
