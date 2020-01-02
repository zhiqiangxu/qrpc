package util

// ByteArena for reduce GC pressure, not concurrent safe
type ByteArena struct {
	alloc             []byte
	chunkAllocMinSize int
	chunkAllocMaxSize int
}

// NewByteArena is ctor for ByteArena
func NewByteArena(chunkAllocMinSize, chunkAllocMaxSize int) *ByteArena {
	return &ByteArena{chunkAllocMinSize: chunkAllocMinSize, chunkAllocMaxSize: chunkAllocMaxSize}
}

// AllocBytes for allocate bytes
func (a *ByteArena) AllocBytes(n int) (bytes []byte) {
	if cap(a.alloc)-len(a.alloc) < n {
		bytes = a.reserveOrAlloc(n)
		if bytes != nil {
			return
		}
	}

	pos := len(a.alloc)
	bytes = a.alloc[pos : pos+n : pos+n]
	a.alloc = a.alloc[:pos+n]

	return
}

// UnsafeReset for reuse
func (a *ByteArena) UnsafeReset() {
	a.alloc = a.alloc[:0]
}

func (a *ByteArena) reserveOrAlloc(n int) (bytes []byte) {

	allocSize := cap(a.alloc) * 2
	if allocSize < a.chunkAllocMinSize {
		allocSize = a.chunkAllocMinSize
	} else if allocSize > a.chunkAllocMaxSize {
		allocSize = a.chunkAllocMaxSize
	}
	if allocSize <= n {
		bytes = make([]byte, 0, n)
		return
	}

	a.alloc = make([]byte, 0, allocSize)
	return
}
