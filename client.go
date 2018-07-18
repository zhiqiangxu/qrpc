package qrpc

import (
	"context"
	"net"
	"sync"
)

// Client defines a qrpc client
type Client struct {
	mu          sync.Mutex
	conf        ConnectionConfig
	connections map[string]*sync.Pool
}

// Connection defines a qrpc connection
// it is not thread safe, because writer is not
type Connection struct {
	net.Conn
	reader       *defaultFrameReader
	writer       FrameWriter
	p            *sync.Pool
	conf         ConnectionConfig
	writeFrameCh chan writeFrameRequest // written by FrameWriter
	subscriber   SubFunc                // there can be only one subscriber because of streamed frames
}

// NewClient creates a Client instance
func NewClient(conf ConnectionConfig) *Client {
	cli := &Client{conf: conf, connections: make(map[string]*sync.Pool)}
	return cli
}

// NewConnection creates a connection without Client
func NewConnection(addr string, conf ConnectionConfig, f func(*Frame)) (*Connection, error) {
	return newConnectionWithPool(addr, conf, nil, SubFunc(f))
}

func newConnectionWithPool(addr string, conf ConnectionConfig, p *sync.Pool, f SubFunc) (*Connection, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	if conf.Ctx == nil {
		conf.Ctx = context.Background()
	}

	c := &Connection{Conn: conn, conf: conf, writeFrameCh: make(chan writeFrameRequest), subscriber: f}
	c.reader = newFrameReader(conf.Ctx, conn, conf.ReadTimeout)
	c.writer = NewFrameWriter(conf.Ctx, c.writeFrameCh)

	go c.readFrames()
	go c.writeFrames()

	return c, nil
}

// GetConn get a connection from Client
func (cli *Client) GetConn(addr string, f func(*Frame)) *Connection {

	cli.mu.Lock()

	p, ok := cli.connections[addr]
	if !ok {
		p = &sync.Pool{}
		newFunc := func() interface{} {

			conn, err := newConnectionWithPool(addr, cli.conf, p, SubFunc(f))
			if err != nil {
				return nil
			}

			return conn
		}
		p.New = newFunc
		cli.connections[addr] = p
	}
	cli.mu.Unlock()

	return p.Get().(*Connection)
}

// Request send a request frame and returns response frame
func (conn *Connection) Request(cmd Cmd, flags PacketFlag, payload []byte) (*Frame, error) {

	return nil, nil
}

// Close internally returns the connection to pool
func (conn *Connection) Close() error {
	if conn.p != nil {
		conn.p.Put(conn)
		return nil
	}

	return conn.Conn.Close()
}

var requestID uint64

func (conn *Connection) readFrames() {
	defer func() {
		conn.Close()
	}()
	for {
		frame, err := conn.reader.ReadFrame()
		if err != nil {
			return
		}

		if frame.Flags&PushFlag != 0 {
			// pushed frame
			if conn.subscriber != nil {
				conn.subscriber(frame)
			}

			return
		}

		// deal with pulled frames
	}
}

// Subscribe register f as callback for pushed message
func (conn *Connection) Subscribe(f func(*Frame)) {
	if conn.subscriber != nil {
		panic("only one subscriber allowed")
	}
	conn.subscriber = f
}
func (conn *Connection) writeFrames() {

}
