package dispatcher

import (
	"testing"
	"unsafe"
)

//go:noinline
func deepFrames(n int, prev *[256]byte) int {
	var pad [256]byte
	pad[n%256] = byte(n)
	if n == 0 {
		return int(pad[0]) + int(prev[0])
	}
	return deepFrames(n-1, &pad) + int(pad[n%256])
}

// stackMoved runs about 10 KB of frames on a fresh goroutine and reports
// whether the runtime had to move (grow) its stack while doing so: a local's
// address changes when the stack is copied.
func stackMoved(pregrow bool) bool {
	moved := make(chan bool)
	go func() {
		if pregrow {
			growDeepStack()
		}
		var local [16]byte
		before := uintptr(unsafe.Pointer(&local))
		sink := deepFrames(36, new([256]byte))
		after := uintptr(unsafe.Pointer(&local))
		local[0] = byte(sink)
		moved <- before != after
	}()
	return <-moved
}

// Config.DeepHandlerStack: after growDeepStack a handler that goes 10 KB deep
// no longer makes the runtime copy its stack on the way down.
func TestGrowDeepStack(t *testing.T) {
	if !stackMoved(false) {
		t.Skip("a fresh goroutine's stack already holds 10 KB here; nothing to pre-grow")
	}
	if stackMoved(true) {
		t.Fatal("the stack was copied again after growDeepStack")
	}
}

// BenchmarkDeepHandlerStack is a goroutine per iteration that goes 10 KB deep,
// with the stack grown as needed and grown once up front.
func BenchmarkDeepHandlerStack(b *testing.B) {
	for _, pre := range []bool{false, true} {
		name := "grow-as-needed"
		if pre {
			name = "grown-up-front"
		}
		b.Run(name, func(b *testing.B) {
			done := make(chan int)
			for i := 0; i < b.N; i++ {
				go func() {
					if pre {
						growDeepStack()
					}
					done <- deepFrames(36, new([256]byte))
				}()
				<-done
			}
		})
	}
}
