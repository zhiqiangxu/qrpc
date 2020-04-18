# qrpcurl

It's basically curl for qRPC servers

## Usage

```
go get github.com/zhiqiangxu/qrpc/tool/qrpcurl

qrpcurl [-f flag] [-w] host:port/cmd payload
```

## Example

```
$ qrpcurl localhost:8888/0 payload
[request]
flag: 0
cmd: 0
payload:
payload

[response]
flag: 0
cmd: 1
payload:
{"err":1,"msg":"flag error","login_info":{"uid":"","max_serve":0,"csids":null},"offline_threads":{"threads":null},"cs_offline_threads":null,"cs_status":0,"api_token":"","device_cookie":""}
```