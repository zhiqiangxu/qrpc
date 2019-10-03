package qrpc

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	mathrand "math/rand"
	"net"
	"runtime"
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

	mu            sync.Mutex
	closed        bool
	rwc           net.Conn
	respes        map[uint64]*response
	loopCtx       context.Context
	loopCancelCtx context.CancelFunc
	loopWG        *sync.WaitGroup

	cs *ConnStreams
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
func NewConnection(addr string, conf ConnectionConfig, f SubFunc) (conn *Connection, err error) {
	var rwc net.Conn
	if conf.OverlayNetwork != nil {
		rwc, err = conf.OverlayNetwork(addr, conf.DialTimeout)
	} else {
		rwc, err = net.DialTimeout("tcp", addr, conf.DialTimeout)
	}

	if err != nil {
		LogError("NewConnection Dial", err)
		return
	}

	conn = newConnection(rwc, []string{addr}, conf, f, false)
	return
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

	rwc, err := net.DialTimeout("tcp", copy[len(copy)-1], conf.DialTimeout)
	if err != nil {
		LogError("initconnect DialTimeout", err)
		rwc = nil
	}
	return newConnection(rwc, copy, conf, f, true)
}

// ClientConnectionInfoKey is context key for ClientConnectionInfo
// used to store custom information
var ClientConnectionInfoKey = &contextKey{"qrpc-clientconnection"}

// ClientConnectionInfo for store info on clientconnection
type ClientConnectionInfo struct {
	CC *Connection
}

func newConnection(rwc net.Conn, addr []string, conf ConnectionConfig, f SubFunc, reconnect bool) *Connection {
	ctx, cancelCtx := context.WithCancel(context.Background())

	c := &Connection{
		rwc: rwc, addrs: addr, conf: conf, subscriber: f,
		writeFrameCh: make(chan writeFrameRequest), respes: make(map[uint64]*response),
		cs: &ConnStreams{}, ctx: ctx, cancelCtx: cancelCtx,
		reconnect: reconnect}

	if conf.Handler != nil {
		c.ctx = context.WithValue(c.ctx, ClientConnectionInfoKey, &ClientConnectionInfo{CC: c})
	}

	if rwc != nil {
		// loopxxx should be paired with rwc
		c.loopCtx, c.loopCancelCtx = context.WithCancel(ctx)
	}
	GoFunc(&c.wg, c.loop)
	return c
}

func (conn *Connection) loop() {
	for {
		if err := conn.connect(); err != nil {
			// connx.Close() was called
			return
		}

		conn.loopWG = &sync.WaitGroup{}
		GoFunc(conn.loopWG, func() {
			conn.readFrames()
		})

		GoFunc(conn.loopWG, func() {
			conn.writeFrames()
		})

		<-conn.loopCtx.Done()
		conn.closeRWC()
		conn.loopWG.Wait()

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
			LogError("connect DialTimeout", err)
		} else {
			ctx, cancelCtx := context.WithCancel(conn.ctx)
			conn.mu.Lock()
			conn.rwc = rwc
			conn.loopCtx = ctx
			conn.loopCancelCtx = cancelCtx
			conn.mu.Unlock()
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

// Wait block until closed
func (conn *Connection) Wait() {
	conn.wg.Wait()
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
		LogError("writeFirstFrame", err)
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
	// ErrConnNotConnected when request not connected
	ErrConnNotConnected = errors.New("connection not connected")
)

func (conn *Connection) writeFirstFrame(cmd Cmd, flags FrameFlag, payload []byte) (uint64, Response, *defaultFrameWriter, error) {
	var (
		requestID uint64
		suc       bool
	)

	requestID = PoorManUUID(true)
	conn.mu.Lock()
	if conn.closed {
		conn.mu.Unlock()
		return 0, nil, nil, ErrConnAlreadyClosed
	}
	if conn.rwc == nil {
		conn.mu.Unlock()
		return 0, nil, nil, ErrConnNotConnected
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
		requestID = PoorManUUID(true)
	}

	if !suc {
		conn.mu.Unlock()
		return 0, nil, nil, ErrNoNewUUID
	}
	resp := &response{Frame: make(chan *Frame, 1)}
	conn.respes[requestID] = resp
	conn.mu.Unlock()

	writer := newFrameWriter(conn)
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

func (conn *Connection) writeFrameBytes(dfw *defaultFrameWriter) error {
	wfr := writeFrameRequest{dfw: dfw, result: make(chan error, 1)}
	select {
	case conn.writeFrameCh <- wfr:
	case <-conn.loopCtx.Done():
		return conn.loopCtx.Err()
	}

	select {
	case err := <-wfr.result:
		return err
	case <-conn.loopCtx.Done():
		return conn.loopCtx.Err()
	}
}

// ResetFrame resets a stream by requestID
func (conn *Connection) ResetFrame(requestID uint64, reason Cmd) error {
	return conn.getWriter().ResetFrame(requestID, reason)
}

// PoorManUUID generate a uint64 uuid
func PoorManUUID(client bool) (result uint64) {
	buf := make([]byte, 8)
	rand.Read(buf)
	result = binary.LittleEndian.Uint64(buf)
	if result == 0 {
		result = math.MaxUint64
	}

	if client {
		result |= 1 //odd for client
	} else {
		result &= math.MaxUint64 - 1 //even for server
	}
	return
}

// close current rwc
func (conn *Connection) closeRWC() {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.rwc == nil {
		return
	}
	conn.rwc.Close()
	conn.rwc = nil // so that connect will Dial another rwc

	for _, v := range conn.respes {
		v.Close()
	}
	conn.respes = make(map[uint64]*response)

	conn.cs.Release()
	conn.cs = &ConnStreams{}
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

func (conn *Connection) readFrames() {

	defer conn.loopCancelCtx()

	// in case closeRWC is already called
	if conn.rwc == nil {
		return
	}
	reader := newFrameReader(conn.loopCtx, conn.rwc, conn.conf.ReadTimeout)
	defer reader.Finalize()

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
			conn.mu.Unlock()
			if conn.conf.Handler != nil {
				if !frame.FromServer() {
					LogError("clientconn get RequestFrame.RequestID not even")
					return
				}
				if !frame.Flags.IsNonBlock() {
					LogError("clientconn get RequestFrame block")
					return
				}
				GoFunc(conn.loopWG, func() {
					defer conn.handleRequestPanic((*RequestFrame)(frame), time.Now())
					conn.conf.Handler.ServeQRPC(conn.getWriter(), (*RequestFrame)(frame))
				})
				continue
			}

			LogError("dangling resp", frame.RequestID)

			continue
		}
		delete(conn.respes, frame.RequestID)
		conn.mu.Unlock()

		resp.SetResponse(frame)

	}
}

func (conn *Connection) handleRequestPanic(frame *RequestFrame, begin time.Time) {
	err := recover()
	if err != nil {
		const size = 64 << 10
		buf := make([]byte, size)
		buf = buf[:runtime.Stack(buf, false)]
		LogError("Connection.handleRequestPanic", err, string(buf))
	}

	s := frame.Stream
	if !s.IsSelfClosed() {
		// send error frame
		writer := conn.getWriter()
		writer.StartWrite(frame.RequestID, 0, StreamRstFlag)
		err := writer.EndWrite()
		if err != nil {
			LogDebug("Connection.send error frame", err, frame)
		}
	}
}

func (conn *Connection) getWriter() FrameWriter {
	return newFrameWriter(conn)
}

func (conn *Connection) writeFrames() (err error) {

	defer conn.loopCancelCtx()

	writer := NewWriterWithTimeout(conn.loopCtx, conn.rwc, conn.conf.WriteTimeout)

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
				s, _ := conn.cs.CreateOrGetStream(conn.loopCtx, requestID, flags)
				if !s.AddOutFrame(requestID, flags) {
					res.result <- ErrWriteAfterCloseSelf
					break
				}
			}

			_, err := writer.Write(dfw.GetWbuf())
			res.result <- err
			if err != nil {
				LogError("clientconn Write", err)
				return err
			}
		case <-conn.loopCtx.Done():
			return conn.loopCtx.Err()
		}
	}
}
