package util

import (
	"crypto/rand"
	"encoding/binary"
	"math"
)

// PoorManUUID generate a uint64 uuid
func PoorManUUID(client bool) (result uint64) {

	result = PoorManUUID2()
	if client {
		result |= 1 //odd for client
	} else {
		result &= math.MaxUint64 - 1 //even for server
	}
	return
}

// PoorManUUID2 doesn't care whether client/server side
func PoorManUUID2() (result uint64) {
	buf := make([]byte, 8)
	rand.Read(buf)
	result = binary.LittleEndian.Uint64(buf)
	if result == 0 {
		result = math.MaxUint64
	}

	return
}
