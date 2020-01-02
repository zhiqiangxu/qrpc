package util

import (
	"hash/crc32"
	"hash/fnv"
)

// CRC32 computes the Castagnoli CRC32 of the given data.
func CRC32(data []byte) (sum32 uint32, err error) {
	hash := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	if _, err = hash.Write(data); err != nil {
		return
	}
	sum32 = hash.Sum32()
	return
}

// FNV32 returns a new 32-bit FNV-1 hash.Hash
func FNV32(data []byte) (sum32 uint32, err error) {
	hash := fnv.New32()
	if _, err = hash.Write(data); err != nil {
		return
	}
	sum32 = hash.Sum32()
	return
}

// FNV32a returns a new 32-bit FNV-1a hash.Hash
func FNV32a(data []byte) (sum32 uint32, err error) {
	hash := fnv.New32()
	if _, err = hash.Write(data); err != nil {
		return
	}
	sum32 = hash.Sum32()
	return
}
