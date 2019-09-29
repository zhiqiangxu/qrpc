# qrpc, tiny but powerful rpc framework

**qrpc** makes it tremendously easy to to perform rpc by offering 4 core features:

> *  `blocking` or `nonblocking`
> *  `streaming` or `nonstreaming`
> *  `server push`
> *  `overlay network` (refer to `ws/README.md` for detail)

By default each frame is `blocking` and `nonstreaming`, this allows traditional block-in-header sequencial behaviour like `http/1.1`, but you can make it behave tremendously different by attach flags to your frames!

------

# Enough talk, let's demo!

## blocking mode

### `server.go`:
```golang
package main
import "github.com/zhiqiangxu/qrpc"

const (
    HelloCmd qrpc.Cmd = iota
    HelloRespCmd
)
func main() {
    handler := qrpc.NewServeMux()
    handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
        writer.StartWrite(request.RequestID, HelloRespCmd, 0)

        writer.WriteBytes(append([]byte("hello world "), request.Payload...))
        writer.EndWrite()
    })
    bindings := []qrpc.ServerBinding{
        qrpc.ServerBinding{Addr: "0.0.0.0:8080", Handler: handler}}
    server := qrpc.NewServer(bindings)
    server.ListenAndServe()
}

```

### `client.go`:

```golang
package main
import (
    "fmt"
    "github.com/zhiqiangxu/qrpc"
)

const (
    HelloCmd qrpc.Cmd = iota
)
func main() {
    conf := qrpc.ConnectionConfig{}

    conn, _ := qrpc.NewConnection("0.0.0.0:8080", conf, nil)

    _, resp, _ := conn.Request(HelloCmd, 0/*no flags*/, []byte("xu"))
    frame, _ := resp.GetFrame()
    fmt.Println("resp is", string(frame.Payload))
}
```

In the above example, server will process each client frames **in sequence order**.

## Nonblocking mode

To use this mode we only need to change 1 line in `client.go`:
```diff
-    _, resp, _ := conn.Request(HelloCmd, 0/*no flags*/, []byte("xu"))
+    _, resp, _ := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu"))
```
In this mode `request` frames will be processed concurrently!

## stream mode

`stream` is like `chunked transfer` in `http`, besides, it's **bidirectional**, we can make either `request` or `response` in `stream`, or we can make both!

### Make `request` in `stream` mode:

### `streamclient.go`:

```golang
package main
import (
    "fmt"
    "github.com/zhiqiangxu/qrpc"
)

const (
    HelloCmd qrpc.Cmd = iota
)
func main() {
    conf := qrpc.ConnectionConfig{}

    conn, _ := qrpc.NewConnection("0.0.0.0:8080", conf, nil)

    writer, resp, _ := conn.StreamRequest(HelloCmd, 0, []byte("first frame"))
    writer.StartWrite(HelloCmd)
    writer.WriteBytes([]byte("last frame"))
    writer.EndWrite(true) // will attach StreamEndFlag
    frame, _ := resp.GetFrame()
    fmt.Println("resp is", string(frame.Payload))
}

```

### `streamserver.go`:
```golang
package main
import (
    "github.com/zhiqiangxu/qrpc"
    "fmt"
)

const (
    HelloCmd qrpc.Cmd = iota
    HelloRespCmd
)
func main() {
    handler := qrpc.NewServeMux()
    handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
        writer.StartWrite(request.RequestID, HelloRespCmd, 0)

        writer.WriteBytes(append([]byte("first frame "), request.Payload...))

        for {
            continueFrames := <-request.FrameCh()
            if continueFrames == nil {
                break
            }
            writer.WriteBytes(append([]byte(" continue frame "), continueFrames.Payload...))
        }
        writer.EndWrite()
    })
    bindings := []qrpc.ServerBinding{
        qrpc.ServerBinding{Addr: "0.0.0.0:8080", Handler: handler}}
    server := qrpc.NewServer(bindings)
    err := server.ListenAndServe()
    if err != nil {
        panic(err)
    }
}
```

In a similar fasion we can also make `response` in `stream` mode:

```golang
package main
import (
    "github.com/zhiqiangxu/qrpc"
    "fmt"
)

const (
    HelloCmd qrpc.Cmd = iota
    HelloRespCmd
)
func main() {
    handler := qrpc.NewServeMux()
    handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
        writer.StartWrite(request.RequestID, HelloRespCmd, qrpc.StreamFlag)
        writer.WriteBytes(append([]byte("first frame "), request.Payload...))
        writer.EndWrite()

        for {
            continueFrames := <-request.FrameCh()
            if continueFrames == nil {
                break
            }

            fmt.Printf("%s\n", continueFrames.Payload)
            if continueFrames.Flags.IsDone() {
                // it's the last frame，so flag it with qrpc.StreamEndFlag
                writer.StartWrite(request.RequestID, HelloRespCmd, qrpc.StreamEndFlag)
            } else {
                // no the last frame，just flag it with qrpc.StreamFlag
                writer.StartWrite(request.RequestID, HelloRespCmd, qrpc.StreamFlag)
            }
            writer.WriteBytes(append([]byte(" continue frame "), continueFrames.Payload...))
            writer.EndWrite()
        }
    })
    bindings := []qrpc.ServerBinding{
        qrpc.ServerBinding{Addr: "0.0.0.0:8080", Handler: handler}}
    server := qrpc.NewServer(bindings)
    err := server.ListenAndServe()
    if err != nil {
        panic(err)
    }
}
```

The key is `StreamFlag`!

## push mode
```golang
package main
import (
    "github.com/zhiqiangxu/qrpc"
    "sync"
)

const (
    HelloCmd qrpc.Cmd = iota
    HelloRespCmd
)
func main() {
    handler := qrpc.NewServeMux()
    handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
        var (
            wg    sync.WaitGroup
        )
        qserver := request.ConnectionInfo().SC.Server()
        pushID := qserver.GetPushID()
        qserver.WalkConn(0, func(writer qrpc.FrameWriter, ci *qrpc.ConnectionInfo) bool {
            qrpc.GoFunc(&wg, func() {
                writer.StartWrite(pushID, HelloCmd, qrpc.PushFlag)
                writer.WriteBytes([]byte("pushed msg"))
                writer.EndWrite()
            })
            return true
        })
        wg.Wait()

        writer.StartWrite(request.RequestID, HelloRespCmd, 0)
        writer.WriteBytes(append([]byte("push done"), request.Payload...))
        writer.EndWrite()
    })
    bindings := []qrpc.ServerBinding{
        qrpc.ServerBinding{Addr: "0.0.0.0:8080", Handler: handler}}
    server := qrpc.NewServer(bindings)
    err := server.ListenAndServe()
    if err != nil {
        panic(err)
    }
}
```

In the above example, server will `push` a message to all connections !

To handle `pushed` message, the relevant change at `client` side is:

```diff
-    conn, _ := qrpc.NewConnection("0.0.0.0:8080", conf, nil)
+    conn, _ := qrpc.NewConnection("0.0.0.0:8080", conf, func(conn *qrpc.Connection, pushedFrame *qrpc.Frame) {
+        fmt.Println(pushedFrame)
+    })
```


There are even more features like `StreamRstFlag`!

## Performance

![avatar](https://raw.githubusercontent.com/zhiqiangxu/qrpc/master/doc/performance.jpg)

About **4** times faster than http!

## Stargazers

[![Stargazers over time](https://starchart.cc/zhiqiangxu/qrpc.svg)](https://starchart.cc/zhiqiangxu/qrpc)
