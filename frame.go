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

// FromClient returns true if frame is from clientconn
func (r *Frame) FromClient() bool {
	return r.RequestID%2 == 1
}

// FromServer returns true if frame is from serveconn
func (r *Frame) FromServer() bool {
	return r.RequestID%2 == 0
}

// RequestFrame is client->server
type RequestFrame Frame

// ConnectionInfo returns the underlying ConnectionInfo
func (r *RequestFrame) ConnectionInfo() *ConnectionInfo {

	return r.Stream.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)

}

// ClientConnectionInfo returns the underlying ClientConnectionInfo
func (r *RequestFrame) ClientConnectionInfo() *ClientConnectionInfo {

	return r.Stream.ctx.Value(ClientConnectionInfoKey).(*ClientConnectionInfo)

}

// Close the underlying connection
func (r *RequestFrame) Close() error {

	if r.FromClient() {
		// RequestID odd means come from client
		ci := r.Stream.ctx.Value(ConnectionInfoKey).(*ConnectionInfo)
		return ci.SC.Close()
	}

	// RequestID even means com from server
	cci := r.Stream.ctx.Value(ClientConnectionInfoKey).(*ClientConnectionInfo)
	cci.CC.closeRWC()
	return nil

}

// FromClient returns true if frame is from clientconn
func (r *RequestFrame) FromClient() bool {
	return (*Frame)(r).FromClient()
}

// Context for RequestFrame
func (r *RequestFrame) Context() context.Context {
	return (*Frame)(r).Context()
}

// FrameCh for RequestFrame
func (r *RequestFrame) FrameCh() <-chan *Frame {
	return (*Frame)(r).FrameCh()
}
