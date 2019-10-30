package codegen

import (
	"context"

	"fmt"

	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/codegen/pb"
)

// MethodCall for rpc
type MethodCall func(context.Context, []byte) ([]byte, error)

// ServiceHandler for rpc dispatch
type ServiceHandler struct {
	errCmd   qrpc.Cmd
	callback map[string]MethodCall
}

// NewServiceHandler creates a ServiceHandler
func NewServiceHandler(errCmd qrpc.Cmd, callback map[string]MethodCall) *ServiceHandler {
	return &ServiceHandler{errCmd: errCmd, callback: callback}
}

// FQMethod for fully qualified method
func FQMethod(ns, method string) string {
	return fmt.Sprintf("%s:%s", ns, method)
}

// ServeQRPC implements qrpc.Handler
func (sh *ServiceHandler) ServeQRPC(w qrpc.FrameWriter, frame *qrpc.RequestFrame) {

	var rpcReq pb.RpcRequest
	err := rpcReq.Unmarshal(frame.Payload)
	if err != nil {
		w.StartWrite(frame.RequestID, sh.errCmd, 0)
		w.WriteBytes([]byte(err.Error()))
		w.EndWrite()
		frame.Close()
		return
	}

	key := FQMethod(rpcReq.NS, rpcReq.Method)

	cb, ok := sh.callback[key]
	if !ok {
		w.StartWrite(frame.RequestID, sh.errCmd, 0)
		w.WriteBytes([]byte("no such service"))
		w.EndWrite()
		frame.Close()
		return
	}

	respBytes, err := cb(frame.Context(), rpcReq.Params)
	if err != nil {
		w.StartWrite(frame.RequestID, sh.errCmd, 0)
		w.WriteBytes([]byte(err.Error()))
		w.EndWrite()
		frame.Close()
		return
	}

	w.StartWrite(frame.RequestID, frame.Cmd, 0)
	w.WriteBytes(respBytes)
	w.EndWrite()

}
