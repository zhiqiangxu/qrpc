package qrpc

import "github.com/zhiqiangxu/util"

const (
	chunkSize = 128 * 1024 * 1024
)

var byteArena = util.NewAtomicByteArena(chunkSize)
