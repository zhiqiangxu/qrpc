# this package is for using qrpc like go channels!

Ideas are brought from https://github.com/docker/libchan

Basically, `Transport.Pipe` is used to **initiate** streams, which returns a `Sender` and `Receiver`, for sending and receiving frames of a specific stream. 

Refer to [`TestChannelStyle`](https://github.com/zhiqiangxu/qrpc/blob/master/test/qrpc_test.go#L457) for details.