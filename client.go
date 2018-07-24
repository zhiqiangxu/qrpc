package qrpc

import (
	"sync"
)

// Client defines a qrpc client
type Client struct {
	mu          sync.Mutex
	conf        ConnectionConfig
	connections map[string]*sync.Pool
}

// NewClient creates a Client instance
func NewClient(conf ConnectionConfig) *Client {
	cli := &Client{conf: conf, connections: make(map[string]*sync.Pool)}
	return cli
}

// GetConn get a connection from Client
func (cli *Client) GetConn(addr string, f func(*Connection, *Frame)) *Connection {

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

	conn, ok := p.Get().(*Connection)
	if !ok {
		return nil
	}
	conn.wakeup()
	conn.subscriber = f
	return conn

}
