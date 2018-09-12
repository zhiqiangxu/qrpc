package qrpc

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	mathrand "math/rand"
	"net"
	"sync"
	"time"
)

const (
	reconnectIntervalAfter1stRound = time.Second * 2
)

// Connection defines a qrpc connection
// it is thread safe
type Connection struct {
	// immutable
	rwc        net.Conn
	addrs      []string
	reconnect  bool
	conf       ConnectionConfig
	subscriber SubFunc // there can be only one subscriber because of streamed frames

	writeFrameCh chan writeFrameRequest // it's never closed so won't panic

	idx int // modified in connect

	// cancelCtx cancels the connection-level context.
	cancelCtx context.CancelFunc
	// ctx is the corresponding context for cancelCtx
	ctx context.Context
	wg  sync.WaitGroup // wait group for goroutines

	mu     sync.Mutex
	closed bool
	respes map[uint64]*response

	cs *connstreams
}

// Response for response frames
type Response interface {
	GetFrame() (*Frame, error)
	GetFrameWithContext(ctx context.Context) (*Frame, error) // frame is valid is error is nil
}

type response struct {
	Frame chan *Frame
}

func (r *response) GetFrame() (*Frame, error) {
	frame := <-r.Frame
	if frame == nil {
		return nil, ErrConnAlreadyClosed
	}
	return frame, nil
}

func (r *response) GetFrameWithContext(ctx context.Context) (*Frame, error) {
	select {
	case frame := <-r.Frame:
		if frame == nil {
			return nil, ErrConnAlreadyClosed
		}
		return frame, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *response) SetResponse(frame *Frame) {
	r.Frame <- frame
}

func (r *response) Close() {
	close(r.Frame)
}

// NewConnection constructs a *Connection without reconnect ability
func NewConnection(addr string, conf ConnectionConfig, f SubFunc) (*Connection, error) {
	rwc, err := net.DialTimeout("tcp", addr, conf.DialTimeout)
	if err != nil {
		logError("NewConnection Dial", err)
		return nil, err
	}

	return newConnection(rwc, []string{addr}, conf, f, false), nil
}

// NewConnectionWithReconnect constructs a *Connection with reconnect ability
func NewConnectionWithReconnect(addrs []string, conf ConnectionConfig, f SubFunc) *Connection {
	var copy []string
	for _, addr := range addrs {
		copy = append(copy, addr)
	}
	mathrand.Shuffle(len(copy), func(i, j int) {
		copy[i], copy[j] = copy[j], copy[i]
	})

	return newConnection(nil, copy, conf, f, true)
}

func newConnection(rwc net.Conn, addr []string, conf ConnectionConfig, f SubFunc, reconnect bool) *Connection {
	ctx, cancelCtx := context.WithCancel(context.Background())
	c := &Connection{
		rwc: rwc, addrs: addr, conf: conf, subscriber: f,
		writeFrameCh: make(chan writeFrameRequest), respes: make(map[uint64]*response),
		cs: newConnStreams(), ctx: ctx, cancelCtx: cancelCtx,
		reconnect: reconnect}

	GoFunc(&c.wg, c.loop)
	return c
}

func (conn *Connection) loop() {
	for {
		if err := conn.connect(); err != nil {
			// connx.Close() was called
			return
		}

		ctx, cancelCtx := context.WithCancel(conn.ctx)
		var wg sync.WaitGroup
		GoFunc(&wg, func() {
			conn.readFrames(ctx, cancelCtx)
		})

		GoFunc(&wg, func() {
			conn.writeFrames(ctx, cancelCtx)
		})

		wg.Wait()

		conn.closeRWC()

		// close & quit if not reconnect; otherwise automatically reconnect
		if !conn.reconnect {
			conn.Close()
			return
		}
	}
}

func (conn *Connection) connect() error {
	// directly return if connection already established
	if conn.rwc != nil {
		return nil
	}

	count := 0
	for {
		addr := conn.addrs[conn.idx%len(conn.addrs)]
		conn.idx++
		count++

		rwc, err := net.DialTimeout("tcp", addr, conn.conf.DialTimeout)
		if err != nil {
			logError("connect DialTimeout", err)
		} else {
			conn.rwc = rwc
			return nil
		}

		if count >= len(conn.addrs) {
			time.Sleep(reconnectIntervalAfter1stRound)
		}

		select {
		case <-conn.ctx.Done():
			return conn.ctx.Err()
		default:
		}
	}

}

// GetWriter return a FrameWriter
func (conn *Connection) GetWriter() FrameWriter {
	return newFrameWriter(conn.ctx, conn.writeFrameCh)
}

// Wait block until closed
func (conn *Connection) Wait() {
	conn.wg.Wait()
}

// StreamWriter is returned by StreamRequest
type StreamWriter interface {
	RequestID() uint64
	StartWrite(cmd Cmd)
	WriteBytes(v []byte)     // v is copied in WriteBytes
	EndWrite(end bool) error // block until scheduled
}

type defaultStreamWriter struct {
	w         *defaultFrameWriter
	requestID uint64
	flags     FrameFlag
}

// NewStreamWriter creates a StreamWriter from FrameWriter
func NewStreamWriter(w FrameWriter, requestID uint64, flags FrameFlag) StreamWriter {
	dfr, ok := w.(*defaultFrameWriter)
	if !ok {
		return nil
	}
	return newStreamWriter(dfr, requestID, flags)
}

func newStreamWriter(w *defaultFrameWriter, requestID uint64, flags FrameFlag) StreamWriter {
	return &defaultStreamWriter{w: w, requestID: requestID, flags: flags}
}

func (dsw *defaultStreamWriter) StartWrite(cmd Cmd) {
	dsw.w.StartWrite(dsw.requestID, cmd, dsw.flags)
}

func (dsw *defaultStreamWriter) RequestID() uint64 {
	return dsw.requestID
}

func (dsw *defaultStreamWriter) WriteBytes(v []byte) {
	dsw.w.WriteBytes(v)
}

func (dsw *defaultStreamWriter) EndWrite(end bool) error {
	return dsw.w.StreamEndWrite(end)
}

// StreamRequest is for streamed request
func (conn *Connection) StreamRequest(cmd Cmd, flags FrameFlag, payload []byte) (StreamWriter, Response, error) {

	flags = flags.ToStream()
	requestID, resp, writer, err := conn.writeFirstFrame(cmd, flags, payload)
	if err != nil {
		logError("writeFirstFrame", err)
		return nil, nil, err
	}
	return newStreamWriter(writer, requestID, flags), resp, nil
}

// Request send a nonstreamed request frame and returns response frame
// error is non nil when write failed
func (conn *Connection) Request(cmd Cmd, flags FrameFlag, payload []byte) (uint64, Response, error) {

	flags = flags.ToNonStream()
	requestID, resp, _, err := conn.writeFirstFrame(cmd, flags, payload)

	return requestID, resp, err
}

var (
	// ErrNoNewUUID when no new uuid available
	ErrNoNewUUID = errors.New("no new uuid available temporary")
	// ErrConnAlreadyClosed when try to operate on an already closed conn
	ErrConnAlreadyClosed = errors.New("connection already closed")
)

func (conn *Connection) writeFirstFrame(cmd Cmd, flags FrameFlag, payload []byte) (uint64, Response, *defaultFrameWriter, error) {
	var (
		requestID uint64
		suc       bool
	)

	requestID = PoorManUUID()
	conn.mu.Lock()
	if conn.closed {
		conn.mu.Unlock()
		return 0, nil, nil, ErrConnAlreadyClosed
	}
	i := 0
	for {
		_, ok := conn.respes[requestID]
		if !ok {
			suc = true
			break
		}

		i++
		if i >= 3 {
			break
		}
		requestID = PoorManUUID()
	}

	if !suc {
		conn.mu.Unlock()
		return 0, nil, nil, ErrNoNewUUID
	}
	resp := &response{Frame: make(chan *Frame, 1)}
	conn.respes[requestID] = resp
	conn.mu.Unlock()

	writer := newFrameWriter(conn.ctx, conn.writeFrameCh)
	writer.StartWrite(requestID, cmd, flags)
	writer.WriteBytes(payload)
	err := writer.EndWrite()

	if err != nil {
		conn.mu.Lock()
		resp, ok := conn.respes[requestID]
		if ok {
			resp.Close()
			delete(conn.respes, requestID)
		}
		conn.mu.Unlock()
		return 0, nil, nil, err
	}

	return requestID, resp, writer, nil
}

// PoorManUUID generate a uint64 uuid
func PoorManUUID() (result uint64) {
	buf := make([]byte, 8)
	rand.Read(buf)
	result = binary.LittleEndian.Uint64(buf)
	if result == 0 {
		result = math.MaxUint64
	}
	return
}

// close current rwc
func (conn *Connection) closeRWC() {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	conn.rwc.Close()
	conn.rwc = nil // so that connect will Dial another rwc

	for _, v := range conn.respes {
		v.Close()
	}
	conn.respes = make(map[uint64]*response)

	conn.cs.Release()
	conn.cs = newConnStreams()
}

// Close closes the qrpc connection
func (conn *Connection) Close() error {

	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.closed {
		return ErrConnAlreadyClosed
	}

	conn.closed = true

	conn.cancelCtx()

	return nil
}

// Done returns the done channel
func (conn *Connection) Done() <-chan struct{} {
	return conn.ctx.Done()
}

// IsClosed tells whether connection is closed
func (conn *Connection) IsClosed() bool {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.closed
}

var requestID uint64

func (conn *Connection) readFrames(ctx context.Context, cancelCtx context.CancelFunc) {

	defer cancelCtx()

	reader := newFrameReader(ctx, conn.rwc, conn.conf.ReadTimeout)

	for {
		frame, err := reader.ReadFrame(conn.cs)
		if err != nil {
			return
		}

		if frame.Flags.IsPush() {
			// pushed frame
			if conn.subscriber != nil {
				conn.subscriber(conn, frame)
			}

			continue
		}

		// deal with pulled frames
		conn.mu.Lock()
		resp, ok := conn.respes[frame.RequestID]
		if !ok {
			logError("dangling resp", frame.RequestID)
			conn.mu.Unlock()
			continue
		}
		delete(conn.respes, frame.RequestID)
		conn.mu.Unlock()

		resp.SetResponse(frame)

	}
}

func (conn *Connection) writeFrames(ctx context.Context, cancelCtx context.CancelFunc) (err error) {

	defer cancelCtx()

	writer := NewWriterWithTimeout(ctx, conn.rwc, conn.conf.WriteTimeout)

	for {
		select {
		case res := <-conn.writeFrameCh:
			dfw := res.dfw
			flags := dfw.Flags()
			requestID := dfw.RequestID()

			if flags.IsRst() {
				s := conn.cs.GetStream(requestID, flags)
				if s == nil {
					res.result <- ErrRstNonExistingStream
					break
				}
				// for rst frame, AddOutFrame returns false when no need to send the frame
				if !s.AddOutFrame(requestID, flags) {
					res.result <- nil
					break
				}
			} else if !flags.IsPush() { // skip stream logic if PushFlag set
				s := conn.cs.CreateOrGetStream(ctx, requestID, flags)
				if !s.AddOutFrame(requestID, flags) {
					res.result <- ErrWriteAfterCloseSelf
					break
				}
			}

			_, err := writer.Write(dfw.GetWbuf())
			res.result <- err
			if err != nil {
				logError("clientconn Write", err)
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
