# qrpc, 轻量级的通用长链接框架

**qrpc** 提供完整的服务端及客户端功能，并支持以下4种特性使得`rpc`变得极为容易:

> *  `阻塞` 或 `非阻塞`
> *  `流式` 或 `非流式`
> *  `主动推送`
> *  `双向调用`

默认是阻塞模式，也就是同一个长链接的请求是串行处理，类似`http/1.1`，但是通过微小的改动就可以切换到其他模式。

此外，`qrpc`还提供了`桥接网络`的特性，该特性使得多协议支持不费吹灰之力，同样的一套代码可以同时跑在`tcp`、`websocket`及任意已有的协议之下，详情参考`ws/README.md`；以及代码生成功能，使得服务的注册和调用极大地便利化，详情参考[`codegen demo`](https://github.com/zhiqiangxu/qrpc-demo/blob/master/codegen/main.go)。

[这里](https://github.com/zhiqiangxu/qrpc/raw/master/doc/qrpc-%E5%A4%9C%E8%AF%BB.pptx)是**ppt**版。

------

# 协议设计

`qrpc`提供`请求->响应`以及`主动推送`两大类的交互能力。

`请求->响应`又分为「`阻塞` 或 `非阻塞`」以及「`流式` 或 `非流式`」。

`qrpc`的`请求`和`响应`有相同的结构：`帧`，即代码中的`Frame`。

每个帧包括`8`字节的`唯一标识`，`1`字节的`flag`，`3`字节的`命令`，以及不超过可配置上限长度的`负荷`。

客户端会为每个`请求帧`自动生成`8`字节的`唯一标识`，服务端对应的`响应帧`会有相同的唯一标识。

通过这种方式，一个长链接可以同时发起多个请求，并且精确地知道每个请求对应的响应结果。

此外，`请求`和`响应`都可以由多个`帧`组成，类似`http`中的`chunked`传输模式，这就是前面提到的`流式`或`非流式`。

而所有关于是否阻塞、是否流式、是否主动推送的元信息，都包含在头部`1`字节的`flag`之中！

具体字段参考[`frame.md`](https://github.com/zhiqiangxu/qrpc/blob/master/doc/frame.md)。

------

# 话不多说，干货开始!


## 阻塞模式

### `server.go`:
```golang
package main
import "github.com/zhiqiangxu/qrpc"

const (
    HelloCmd qrpc.Cmd = iota
    HelloRespCmd
)
func main() {
    // handler的作用是路由，根据请求帧的命令，分发到不同的处理子函数
    handler := qrpc.NewServeMux()
    // 注册HelloCmd命令对应的子函数
    handler.HandleFunc(HelloCmd, func(writer/*用于回写响应*/ qrpc.FrameWriter, request/*当前请求的相关信息*/ *qrpc.RequestFrame) {
        // 响应帧和请求帧有相同的唯一标识，并且这里把响应帧的命令设置为HelloRespCmd，会更方便调试
        writer.StartWrite(request.RequestID, HelloRespCmd, 0)

        // 负荷部分为：hello world + 请求帧的原始负荷
        writer.WriteBytes(append([]byte("hello world "), request.Payload...))

        // 前面的StartWrite和WriteBytes其实是构建响应帧的过程
        // 构建完毕后通过EndWrite触发实际的回写
        writer.EndWrite()
    })
    // ServerBinding用于配制想监听的端口以及对应的处理函数，如果想监听多个端口，提供多个即可
    bindings := []qrpc.ServerBinding{
        qrpc.ServerBinding{Addr: "0.0.0.0:8080", Handler: handler}}
    // 构建server
    server := qrpc.NewServer(bindings)
    // 开始监听
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
    // 采用默认配置
    conf := qrpc.ConnectionConfig{}

    // 建立一个qrpc的长链接
    conn, _ := qrpc.NewConnection("0.0.0.0:8080", conf, nil)

    // 发起一个命令为HelloCmd的请求帧，flag空，负荷为xu
    _, resp, _ := conn.Request(HelloCmd, 0/*no flags*/, []byte("xu"))
    // 获取响应
    frame, _ := resp.GetFrame()
    // 打印响应负荷
    fmt.Println("resp is", string(frame.Payload))
}
```

上面的例子中，由于`flag`为空，所以服务端会采用默认的串行处理模式。

------

## 非阻塞模式

要使用该模式，只需要修改`client.go`的一行代码:
```diff
-    _, resp, _ := conn.Request(HelloCmd, 0/*no flags*/, []byte("xu"))
+    _, resp, _ := conn.Request(HelloCmd, qrpc.NBFlag, []byte("xu"))
```

这样服务端便会并行处理这个长链接发来的请求！

------

## 流式

类似`http`中的`chunked`传输模式，不论请求还是响应，都可以拆成多个帧。

下面按`流式请求`和`流式响应`分别介绍。

### 流式请求:

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
     // 采用默认配置
    conf := qrpc.ConnectionConfig{}

    // 建立一个qrpc的长链接
    conn, _ := qrpc.NewConnection("0.0.0.0:8080", conf, nil)

    // 采用流式发送HelloCmd请求，第一个请求帧的负荷是first frame
    writer, resp, _ := conn.StreamRequest(HelloCmd, 0, []byte("first frame"))
    // 构建第二个请求帧，负荷是last frame
    writer.StartWrite(HelloCmd)
    writer.WriteBytes([]byte("last frame"))
    // 发送请求，并标记流式结束
    writer.EndWrite(true) // will attach StreamEndFlag
    // 获取响应
    frame, _ := resp.GetFrame()
    // 打印响应负荷
    fmt.Println("resp is", string(frame.Payload))
}

```

### `streamserver.go`:
```golang
package main
import (
    "github.com/zhiqiangxu/qrpc"
)

const (
    HelloCmd qrpc.Cmd = iota
    HelloRespCmd
)
func main() {
    handler := qrpc.NewServeMux()
    handler.HandleFunc(HelloCmd, func(writer qrpc.FrameWriter, request *qrpc.RequestFrame) {
        // 首帧的处理类似非流式，只是EndWrite最后才会调用
        writer.StartWrite(request.RequestID, HelloRespCmd, 0)

        writer.WriteBytes(append([]byte("first frame "), request.Payload...))

        // 循环取出流式请求中的剩余帧
        for {
            continueFrames := <-request.FrameCh()
            // continueFrames为nil表示该请求的所有帧获取完毕
            if continueFrames == nil {
                break
            }
            // 将后续帧的负荷追加到响应中去
            writer.WriteBytes(append([]byte(" continue frame "), continueFrames.Payload...))
        }
        // 响应帧构建完毕，回写给客户端
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
------

### 流式响应:

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
        // 构建流式响应的第一帧
        writer.StartWrite(request.RequestID, HelloRespCmd, qrpc.StreamFlag)
        writer.WriteBytes(append([]byte("first frame "), request.Payload...))
        // 第一帧构建完毕，发送
        writer.EndWrite()

        for {
            // 获取流式请求的后续帧
            continueFrames := <-request.FrameCh()
            if continueFrames == nil {
                break
            }

            fmt.Printf("%s\n", continueFrames.Payload)
            // 构建流式响应的后续帧
            if continueFrames.Flags.IsDone() {
                // 最后一帧，flag标记为qrpc.StreamEndFlag
                writer.StartWrite(request.RequestID, HelloRespCmd, qrpc.StreamEndFlag)
            } else {
                // 不是最后一帧，标记为qrpc.StreamFlag
                writer.StartWrite(request.RequestID, HelloRespCmd, qrpc.StreamFlag)
            }
            
            writer.WriteBytes(append([]byte(" continue frame "), continueFrames.Payload...))
            // 后续帧构建完毕，发送
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

关键是`qrpc.StreamFlag`!

------

## 推送模式

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
        // 遍历所有长链接
        qserver.WalkConn(0, func(writer qrpc.FrameWriter, ci *qrpc.ConnectionInfo) bool {
            qrpc.GoFunc(&wg, func() {
                // PushFlag表示主动推送
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

上述代码中，`HelloCmd`的处理子函数将给每个长链接推送一条消息！

客户端处理推送消息的方式如下：

```diff
-    conn, _ := qrpc.NewConnection("0.0.0.0:8080", conf, nil)
+    conn, _ := qrpc.NewConnection("0.0.0.0:8080", conf, func(conn *qrpc.Connection, pushedFrame *qrpc.Frame) {
+        fmt.Println(pushedFrame)
+    })
```

## 双向调用

前面的`demo`都是`client`调用`server`，其实`client`也可以注册回调让`server`调，参考[测试用例](https://github.com/zhiqiangxu/qrpc/blob/master/test/qrpc_test.go)中的`TestClientHandler`。

------

## Performance

![avatar](https://raw.githubusercontent.com/zhiqiangxu/qrpc/master/doc/performance.jpg)

性能大概是`http`的 **4** 倍!

------

## 社区

目前我们暂时只针对中国的用户，所以采用了微信群的交流方式，下面是二维码，有兴趣的同学可以扫码加入：

> PS：扫码请注明来意，比如：学习`qrpc`或者`go`爱好者

<table>
	<tbody>
		<tr>
			<td align="center" valign="middle">
				<img width = "200" src="https://raw.githubusercontent.com/zhiqiangxu/qrpc/master/doc/wechat.png"/>
			</td>
		</tr>
	</tbody>
</table>


## Stargazers

[![Stargazers over time](https://starchart.cc/zhiqiangxu/qrpc.svg)](https://starchart.cc/zhiqiangxu/qrpc)
