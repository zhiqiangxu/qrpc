package sugar

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/sugar/pb"
	"github.com/zhiqiangxu/util"
)

// MethodCall for rpc
type MethodCall func(context.Context, []byte) ([]byte, error)

// Service for auto rpc
type Service struct {
	mu       sync.RWMutex
	errCmd   qrpc.Cmd
	callback map[string]MethodCall
}

// NewService creates a Service
func NewService(s interface{}, errCmd qrpc.Cmd) *Service {
	return &Service{errCmd: errCmd, callback: make(map[string]MethodCall)}
}

// RegisterService for register service
func (svc *Service) RegisterService(ns string, s interface{}) {
	methods := util.ScanMethods(s)
	if len(methods) == 0 {
		panic(fmt.Sprintf("no method scanned for %s", ns))
	}
	for _, method := range methods {
		checkFuncIO(method)
	}

	svc.mu.Lock()
	defer svc.mu.Unlock()
	for name, method := range methods {

		inTypes := util.FuncInputTypes(method)
		outTypes := util.FuncOutputTypes(method)

		inTypePtrIsMarshaler := reflect.PtrTo(inTypes[1]).Implements(marshalerType)
		outTypePtrIsMarshaler := reflect.PtrTo(outTypes[0]).Implements(marshalerType)
		svc.callback[svc.uri(ns, name)] = func(ctx context.Context, payload []byte) (bytes []byte, err error) {

			input := util.InstancePtrByType(inTypes[1])
			if inTypePtrIsMarshaler {
				err = input.(Marshaler).Unmarshal(payload)
			} else {
				err = json.Unmarshal(payload, input)
			}

			if err != nil {
				return
			}

			out := method.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(input).Elem()})

			if outTypePtrIsMarshaler {
				bytes, err = util.InstancePtrByClone(out[0]).(Marshaler).Marshal()
			} else {
				bytes, err = json.Marshal(out[0].Interface())
			}

			return
		}
	}

}

func (svc *Service) uri(ns, method string) string {
	return fmt.Sprintf("%s:%s", ns, method)
}

// ServeQRPC implements qrpc.Handler
func (svc *Service) ServeQRPC(w qrpc.FrameWriter, frame *qrpc.RequestFrame) {
	var rpcReq pb.RpcRequest
	err := rpcReq.Unmarshal(frame.Payload)
	if err != nil {
		w.StartWrite(frame.RequestID, svc.errCmd, 0)
		w.WriteBytes([]byte(err.Error()))
		w.EndWrite()
		frame.Close()
		return
	}

	uri := svc.uri(rpcReq.Service, rpcReq.Method)

	svc.mu.RLock()
	defer svc.mu.RUnlock()

	cb, ok := svc.callback[uri]
	if !ok {
		w.StartWrite(frame.RequestID, svc.errCmd, 0)
		w.WriteBytes([]byte("no such service"))
		w.EndWrite()
		frame.Close()
		return
	}

	respBytes, err := cb(frame.Context(), rpcReq.Params)
	if err != nil {
		w.StartWrite(frame.RequestID, svc.errCmd, 0)
		w.WriteBytes([]byte(err.Error()))
		w.EndWrite()
		frame.Close()
	}

	w.StartWrite(frame.RequestID, frame.Cmd, 0)
	w.WriteBytes(respBytes)
	w.EndWrite()

}
