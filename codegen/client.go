package codegen

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/codegen/pb"
	"github.com/zhiqiangxu/util"
)

// Client for auto rpc
type Client struct {
	cmd    qrpc.Cmd
	errCmd qrpc.Cmd
	idx    uint32
	conns  []*qrpc.Connection
}

// NewClient returns a Client
func NewClient(cmd, errCmd qrpc.Cmd, addrs []string, conf qrpc.ConnectionConfig) *Client {
	var (
		conns []*qrpc.Connection
	)
	for _, addr := range addrs {
		conn := qrpc.NewConnectionWithReconnect([]string{addr}, conf, nil)
		conns = append(conns, conn)
	}
	return &Client{cmd: cmd, errCmd: errCmd, conns: conns}
}

// ErrServiceNA when service not available
var ErrServiceNA = errors.New("service unavailable")

// Request sends a request and returns the response
func (client *Client) Request(ctx context.Context, ns, name string, inBytes []byte) (outBytes []byte, err error) {

	rpcReq := pb.RpcRequest{NS: ns, Method: name, Params: inBytes}
	bytes, err := rpcReq.Marshal()
	if err != nil {
		return
	}

	var (
		resp      qrpc.Response
		frame     *qrpc.Frame
		requestID uint64
	)
	idx := atomic.AddUint32(&client.idx, 1)
	for i := range client.conns {
		j := (i + int(idx)) % len(client.conns)
		requestID, resp, err = client.conns[j].Request(client.cmd, qrpc.NBFlag, bytes)
		if err != nil {
			continue
		}
		frame, err = resp.GetFrameWithContext(ctx)
		if err != nil {
			// err is caused by ctx cancel
			client.conns[j].ResetFrame(requestID, 0)
			return
		}
		if frame.Cmd == client.errCmd {
			err = errors.New(util.String(frame.Payload))
			return
		}

		outBytes = frame.Payload
		frame.Payload = nil

		return
	}

	err = ErrServiceNA
	return
}
