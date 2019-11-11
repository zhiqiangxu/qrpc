## qrpc frame

`qrpc`采用`大端序`。
先是`4`字节`uint32`，表示整个包除了该`uint32`的长度，`length`
再是`8`字节`uint64`，`stream`的唯一`ID`
再是`1`字节`uint8`，一些flag，表示`是否并行`，`是否压缩(压缩方式待定)`等
再是`3`字节`uint24`，表示哪个`cmd`
再是`length-12`字节的`payload`