package sugar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/zhiqiangxu/qrpc"
	"github.com/zhiqiangxu/qrpc/sugar/pb"
	"github.com/zhiqiangxu/util"
)

// Client for auto rpc
type Client struct {
	cmd    qrpc.Cmd
	errCmd qrpc.Cmd
	idx    int32
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

var ctxType = util.TypeByPointer((*context.Context)(nil))
var outputType = util.TypeByPointer((*Output)(nil))
var marshalerType = util.TypeByPointer((*Marshaler)(nil))

// UserService for register service
func (client *Client) UserService(ns string, service interface{}) {
	fields := util.StructFields(service, func(_ string, f reflect.Value) bool {
		return f.Kind() == reflect.Func
	})
	for name, field := range fields {
		checkFuncIO(field)

		util.ReplaceFuncVar(field, client.generateFunc(ns, name, util.FuncInputTypes(field)[1], util.FuncOutputTypes(field)[0]))
	}
}

func checkFuncIO(fun reflect.Value) {
	inTypes := util.FuncInputTypes(fun)
	if len(inTypes) != 2 {
		panic(fmt.Sprintf("input count not 2:%v", fun))
	}
	if !inTypes[0].Implements(ctxType) {
		panic(fmt.Sprintf("first input not impl context.Context:%v", fun))
	}

	outTypes := util.FuncOutputTypes(fun)
	if len(outTypes) != 1 {
		panic(fmt.Sprintf("output count not 1:%v", fun))
	}

	if !reflect.PtrTo(outTypes[0]).Implements(outputType) {
		panic(fmt.Sprintf("output not impl Output:%v %v", fun, outTypes[0]))
	}
}

func (client *Client) generateFunc(ns, name string, inType, outType reflect.Type) (fn func(in []reflect.Value) (out []reflect.Value)) {
	inTypePtrIsMarshaler := reflect.PtrTo(inType).Implements(marshalerType)
	outTypePtrIsMarshaler := reflect.PtrTo(outType).Implements(marshalerType)

	fn = func(in []reflect.Value) (out []reflect.Value) {
		ctx := in[0].Interface().(context.Context)
		var input interface{}
		if inTypePtrIsMarshaler {
			input = util.InstancePtrByClone(in[1])
		} else {
			input = in[1].Interface()
		}

		output := util.InstancePtrByType(outType).(Output)
		bytes, err := client.request(ctx, ns, name, input, inTypePtrIsMarshaler)
		if err != nil {
			output.SetError(err)
			out = append(out, reflect.ValueOf(output).Elem())
			return
		}

		if outTypePtrIsMarshaler {
			err = output.(Marshaler).Unmarshal(bytes)
		} else {
			err = json.Unmarshal(bytes, output)
		}

		if err != nil {
			output.SetError(err)
		}

		out = append(out, reflect.ValueOf(output).Elem())

		return
	}
	return
}

// ErrServiceNA when service not available
var ErrServiceNA = errors.New("service unavailable")

func (client *Client) request(ctx context.Context, ns, name string, input interface{}, inTypePtrIsMarshaler bool) (bytes []byte, err error) {

	if inTypePtrIsMarshaler {
		// input is ptr type in this case
		bytes, err = input.(Marshaler).Marshal()
	} else {
		bytes, err = json.Marshal(input)
	}
	if err != nil {
		return
	}
	rpcReq := pb.RpcRequest{Service: ns, Method: name, Params: bytes}
	bytes, err = rpcReq.Marshal()
	if err != nil {
		return
	}

	var (
		resp      qrpc.Response
		frame     *qrpc.Frame
		requestID uint64
	)
	idx := atomic.AddInt32(&client.idx, 1)
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
			err = errors.New(qrpc.String(frame.Payload))
			return
		}

		bytes = frame.Payload
		frame.Payload = nil

		return
	}

	err = ErrServiceNA
	return
}
