package linkgrpc

import (
	"errors"
	"sync"
	"testing"
)

func TestSendQueue_OrderBatchingAndError(t *testing.T) {
	var mu sync.Mutex
	var got []int
	var batches int
	gate := make(chan struct{})
	q := &sendQueue[int]{
		write: func(i int) error {
			if i == 0 {
				<-gate // hold the first write so the rest queue up behind it
			}
			mu.Lock()
			got = append(got, i)
			mu.Unlock()
			return nil
		},
		batch: func(b []int) error {
			mu.Lock()
			batches++
			got = append(got, b...)
			mu.Unlock()
			return nil
		},
	}
	done := make(chan struct{})
	go func() { _ = q.push(0); close(done) }()
	for { // wait until the first write is in progress
		q.mu.Lock()
		a := q.active
		q.mu.Unlock()
		if a {
			break
		}
	}
	for i := 1; i <= 300; i++ {
		if err := q.push(i); err != nil {
			t.Fatal(err)
		}
	}
	close(gate)
	<-done
	for { // the queue empties (possibly on a hand-over goroutine)
		q.mu.Lock()
		a := q.active
		q.mu.Unlock()
		if !a {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 301 {
		t.Fatalf("got %d frames", len(got))
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("frame %d out of order: %d", i, v)
		}
	}
	if batches == 0 {
		t.Fatal("queued frames were not batched")
	}

	boom := errors.New("boom")
	bad := &sendQueue[int]{write: func(int) error { return boom }}
	if err := bad.push(1); !errors.Is(err, boom) {
		t.Fatalf("first error: %v", err)
	}
	if err := bad.push(2); !errors.Is(err, boom) {
		t.Fatalf("sticky error: %v", err)
	}
}

// B4: a write error met while flushing other goroutines' frames must not be
// returned to the owner whose own frame was written.
func TestSendQueue_ForeignWriteErrorNotAttributedToOwner(t *testing.T) {
	boom := errors.New("boom")
	gate := make(chan struct{})
	q := &sendQueue[int]{write: func(i int) error {
		if i == 0 {
			<-gate
			return nil
		}
		return boom // every later frame fails
	}}
	res := make(chan error, 1)
	go func() { res <- q.push(0) }()
	for {
		q.mu.Lock()
		a := q.active
		q.mu.Unlock()
		if a {
			break
		}
	}
	if err := q.push(1); err != nil { // queued behind the owner: nil
		t.Fatalf("queued push: %v", err)
	}
	close(gate)
	if err := <-res; err != nil {
		t.Fatalf("owner's frame 0 was written, but push returned %v", err)
	}
	if err := q.push(2); !errors.Is(err, boom) {
		t.Fatalf("stream must stay broken: %v", err)
	}
}

func TestSendQueue_BatchByteCap(t *testing.T) {
	var sizes []int
	gate := make(chan struct{})
	q := &sendQueue[int]{
		write: func(i int) error {
			if i == 0 {
				<-gate
			}
			return nil
		},
		batch: func(b []int) error { sizes = append(sizes, len(b)); return nil },
		size:  func(int) int { return maxBatchBytes / 4 },
	}
	done := make(chan struct{})
	go func() { _ = q.push(0); close(done) }()
	for {
		q.mu.Lock()
		a := q.active
		q.mu.Unlock()
		if a {
			break
		}
	}
	for i := 1; i <= 10; i++ {
		_ = q.push(i)
	}
	close(gate)
	<-done
	for {
		q.mu.Lock()
		a := q.active
		q.mu.Unlock()
		if !a {
			break
		}
	}
	for _, n := range sizes {
		if n > 4 {
			t.Fatalf("batch of %d frames exceeds the byte cap: %v", n, sizes)
		}
	}
}
