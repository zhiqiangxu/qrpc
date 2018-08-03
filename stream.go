package qrpc

import (
	"context"
	"sync"
	"sync/atomic"
)

// connstreams hosts all streams on connection
type connstreams struct {
	streams     sync.Map // map[uint64]*stream
	pushstreams sync.Map // map[uint64]*stream

}

func newConnStreams() *connstreams {
	return &connstreams{}
}

// GetStream tries to get the associated stream, called by framereader for rst frame
func (cs *connstreams) GetStream(requestID uint64, flags FrameFlag) *stream {

	var target *sync.Map
	if flags.IsPush() {
		target = &cs.pushstreams
	} else {
		target = &cs.streams
	}

	v, ok := target.Load(requestID)
	if !ok {
		return nil
	}
	return v.(*stream)
}

// get or create the associated stream atomically
// if PushFlag is set, should only call CreateOrGetStream if caller is framereader
func (cs *connstreams) CreateOrGetStream(ctx context.Context, requestID uint64, flags FrameFlag) *stream {

	var target *sync.Map
	if flags.IsPush() {
		target = &cs.pushstreams
	} else {
		target = &cs.streams
	}

	v, _ := target.LoadOrStore(requestID, newStream(ctx, requestID, func() {
		target.Delete(requestID)
	}))
	return v.(*stream)

}

type stream struct {
	ID         uint64
	frameCh    chan *Frame // always not nil
	ctx        context.Context
	cancelFunc context.CancelFunc

	closedSelf  int32
	closedPeer  int32
	fullClosed  int32
	binded      bool
	fullCloseCh chan struct{}
	closeNotify func() // called when stream is fully closed
}

func newStream(ctx context.Context, requestID uint64, closeNotify func()) *stream {
	ctx, cancelFunc := context.WithCancel(ctx)
	s := &stream{ID: requestID, frameCh: make(chan *Frame), ctx: ctx, cancelFunc: cancelFunc, fullCloseCh: make(chan struct{}), closeNotify: closeNotify}

	return s
}

func (s *stream) IsSelfClosed() bool {

	return atomic.LoadInt32(&s.closedSelf) != 0

}

// returns true if the stream has not been binded to any frame yet
// streamreader will first call TryBind, and if fail, call AddInFrame
// not ts
func (s *stream) TryBind(firstFrame *Frame) bool {

	if s.binded {
		return false
	}
	s.binded = true

	s.closePeerIfNeeded(firstFrame.Flags)

	firstFrame.Stream = s

	return true
}

// Done returns a channel for caller to wait for stream close
// when channel returned, stream is cleanly closed
func (s *stream) Done() <-chan struct{} {
	return s.fullCloseCh
}

// AddInFrame tries to add an inframe to stream
// framereader will call AddInFrame after TryBind fails
// return value means whether accepted by stream
// if not accepted, framereader should wait until stream closed,
// and call DeleteStream then CreateOrGetStream again
func (s *stream) AddInFrame(frame *Frame) bool {
	if atomic.LoadInt32(&s.closedPeer) != 0 {
		return false
	}

	select {
	case s.frameCh <- frame:
		s.closePeerIfNeeded(frame.Flags)
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *stream) closePeerIfNeeded(flags FrameFlag) {
	if flags.IsPush() {
		atomic.StoreInt32(&s.closedSelf, 1)
	}
	if !flags.IsDone() {
		return
	}

	atomic.StoreInt32(&s.closedPeer, 1)
	close(s.frameCh)
	if atomic.LoadInt32(&s.closedSelf) != 0 {
		s.afterDone()
	}
}

// AddInFrame tries to add an nonpushed outframe to stream
// framewriter will blindly call AddOutFrame
// return value means whether accepted by stream
// if not accepted, framewriter should throw away the frame
// if accepted, framewriter can go ahead actually sending the frame
// for rst frame, return false mean there's no need to send rst frame
func (s *stream) AddOutFrame(requestID uint64, flags FrameFlag) bool {

	isRst := flags.IsRst()

	if isRst {
		atomic.StoreInt32(&s.closedSelf, 1)
		if atomic.LoadInt32(&s.closedPeer) != 0 {
			s.afterDone()
			return false
		}
		return true
	}

	if atomic.LoadInt32(&s.closedSelf) != 0 {
		return false
	}

	if !flags.IsDone() {
		return true
	}

	atomic.StoreInt32(&s.closedSelf, 1)
	if atomic.LoadInt32(&s.closedPeer) != 0 {
		s.afterDone()
	}
	return true
}

func (s *stream) ResetByPeer() {
	swapped := atomic.CompareAndSwapInt32(&s.closedPeer, 0, 1)
	if swapped {
		close(s.frameCh)
	}

	s.reset()
}

func (s *stream) afterDone() {

	swapped := atomic.CompareAndSwapInt32(&s.fullClosed, 0, 1)

	if swapped {
		s.reset()
		s.closeNotify()
		close(s.fullCloseCh)
	}

}

func (s *stream) reset() {

	s.cancelFunc()

}
