package qrpc

func getg() *g

type stack struct {
	lo uintptr
	hi uintptr
}
type g struct {
	// Stack parameters.
	// stack describes the actual stack memory: [stack.lo, stack.hi).
	// stackguard0 is the stack pointer compared in the Go stack growth prologue.
	// It is stack.lo+StackGuard normally, but can be StackPreempt to trigger a preemption.
	// stackguard1 is the stack pointer compared in the C stack growth prologue.
	// It is stack.lo+StackGuard on g0 and gsignal stacks.
	// It is ~0 on other goroutine stacks, to trigger a call to morestackc (and crash).
	stack stack // offset known to runtime/cgo
}

// StackSize calculates current stack size
func StackSize() uintptr {
	g := getg()
	return g.stack.hi - g.stack.lo
}
