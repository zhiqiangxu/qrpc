package channel

import (
	"context"

	"github.com/zhiqiangxu/qrpc"
)

type (
	// Transport for initiate a new stream
	Transport interface {
		Pipe() (Sender, Receiver, error)
	}

	// Sender for send message
	Sender interface {
		Send(ctx context.Context, cmd qrpc.Cmd, msg interface{}, end bool) error
		// signal for the end of stream
		End() error
	}

	// Receiver for receive message
	Receiver interface {
		Receive(ctx context.Context, cmd *qrpc.Cmd, msg interface{}) error
	}
)
