package qrpc

import (
	"context"
	"sync"
	"sync/atomic"
	"unsafe"

	"go.uber.org/zap"
)

// ConnStreams hosts all streams on connection
type ConnStreams struct {
	streams     sync.Map // map[uint64]*Stream
	pushstreams sync.Map // map[uint64]*Stream

}

// GetStream tries to get the associated stream, called by framereader for rst frame
func (cs *ConnStreams) GetStream(requestID uint64, flags FrameFlag) *Stream {

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
	return v.(*Stream)
}

// CreateOrGetStream get or create the associated stream atomically
// if PushFlag is set, should only call CreateOrGetStream if caller is framereader
func (cs *ConnStreams) CreateOrGetStream(ctx context.Context, requestID uint64, flags FrameFlag) (*Stream, bool) {

	var target *sync.Map
	if flags.IsPush() {
		target = &cs.pushstreams
	} else {
		target = &cs.streams
	}

	s := newStream(ctx, requestID, func() {
		l.Debug("close stream", zap.Uintptr("cs", uintptr(unsafe.Pointer(cs))), zap.Uint64("requestID", requestID), zap.Uint8("flags", uint8(flags)))
		target.Delete(requestID)
	})
	v, loaded := target.LoadOrStore(requestID, s)
	if loaded {
		// otherwise ml happens
		// TODO refactor stream!
		s.reset()
	}
	return v.(*Stream), loaded

}

// Release all streams
// it is called when absolutely sure that no more streams will get in/out
func (cs *ConnStreams) Release() {
	cs.pushstreams.Range(func(k, v interface{}) bool {
		v.(*Stream).Release()
		return true
	})
	cs.streams.Range(func(k, v interface{}) bool {
		v.(*Stream).Release()
		return true
	})
}

// Stream is like session within one requestID
type Stream struct {
	ID         uint64
	frameCh    chan *Frame // always not nil, closed when peer is done
	ctx        context.Context
	cancelFunc context.CancelFunc

	closedSelf  int32
	closedPeer  int32
	fullClosed  int32
	binded      bool
	fullCloseCh chan struct{}
	closeNotify func() // called when stream is fully closed
}

func newStream(ctx context.Context, requestID uint64, closeNotify func()) *Stream {
	ctx, cancelFunc := context.WithCancel(ctx)
	s := &Stream{ID: requestID, frameCh: make(chan *Frame), ctx: ctx, cancelFunc: cancelFunc, fullCloseCh: make(chan struct{}), closeNotify: closeNotify}

	return s
}

// IsSelfClosed checks whether self is closed
func (s *Stream) IsSelfClosed() bool {

	return atomic.LoadInt32(&s.closedSelf) != 0

}

// TryBind returns true if the stream has not been binded to any frame yet
// streamreader will first call TryBind, and if fail, call AddInFrame
// not ts
func (s *Stream) TryBind(firstFrame *Frame) bool {

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
func (s *Stream) Done() <-chan struct{} {
	return s.fullCloseCh
}

// AddInFrame tries to add an inframe to stream
// framereader will call AddInFrame after TryBind fails
// return value means whether accepted by stream
// if not accepted, framereader should wait until stream closed,
// and call DeleteStream then CreateOrGetStream again
func (s *Stream) AddInFrame(frame *Frame) bool {
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

func (s *Stream) closePeerIfNeeded(flags FrameFlag) {
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

// AddOutFrame tries to add an nonpushed outframe to stream
// framewriter will blindly call AddOutFrame
// return value means whether accepted by stream
// if not accepted, framewriter should throw away the frame
// if accepted, framewriter can go ahead actually sending the frame
// for rst frame, return false mean there's no need to send rst frame
func (s *Stream) AddOutFrame(requestID uint64, flags FrameFlag) bool {

	isRst := flags.IsRst()

	if isRst {
		if atomic.LoadInt32(&s.closedSelf) != 0 {
			return false
		}
		atomic.StoreInt32(&s.closedSelf, 1)
		if atomic.LoadInt32(&s.closedPeer) != 0 {
			s.afterDone()
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

// ResetByPeer resets a stream by peer
func (s *Stream) ResetByPeer() {
	swapped := atomic.CompareAndSwapInt32(&s.closedPeer, 0, 1)
	if swapped {
		close(s.frameCh)
	}

	s.reset()
}

func (s *Stream) afterDone() {

	swapped := atomic.CompareAndSwapInt32(&s.fullClosed, 0, 1)

	if swapped {
		s.reset()
		s.closeNotify()
		close(s.fullCloseCh)
	}

}

func (s *Stream) reset() {

	s.cancelFunc()

}

// Release is only called by clientconn for reconnect
// it's only safe to call when read/write goroutines are finished beforehand
func (s *Stream) Release() {
	s.ResetByPeer()
	s.afterDone()
}
