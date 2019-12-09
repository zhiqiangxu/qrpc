package qrpc

import "github.com/zhiqiangxu/util"

const (
	chunkSize = 512 * 1024 * 1024
)

var byteArena = util.NewAtomicByteArena(chunkSize)
