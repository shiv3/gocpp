package linkgrpc

import (
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// lingerLog records, in fake time, when each frame was pushed and written.
type lingerLog struct {
	mu      sync.Mutex
	pushed  map[int]time.Time
	delay   map[int]time.Duration
	order   []int
	batches []int // frames per write
}

func newLingerQueue(linger time.Duration) (*sendQueue[int], *lingerLog) {
	l := &lingerLog{pushed: map[int]time.Time{}, delay: map[int]time.Duration{}}
	wrote := func(b ...int) error {
		l.mu.Lock()
		defer l.mu.Unlock()
		for _, i := range b {
			l.delay[i] = time.Since(l.pushed[i])
			l.order = append(l.order, i)
		}
		l.batches = append(l.batches, len(b))
		return nil
	}
	q := &sendQueue[int]{
		write:  func(i int) error { return wrote(i) },
		batch:  func(b []int) error { return wrote(b...) },
		linger: linger,
	}
	return q, l
}

func (l *lingerLog) push(t *testing.T, q *sendQueue[int], i int) {
	t.Helper()
	l.mu.Lock()
	l.pushed[i] = time.Now()
	l.mu.Unlock()
	if err := q.push(i); err != nil {
		t.Fatalf("push %d: %v", i, err)
	}
}

func (l *lingerLog) check(t *testing.T, n int) (maxDelay time.Duration, delayed int) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.order) != n {
		t.Fatalf("%d of %d frames written", len(l.order), n)
	}
	for i, v := range l.order {
		if v != i {
			t.Fatalf("frame %d written in position %d", v, i)
		}
		if d := l.delay[i]; d > 0 {
			delayed++
			maxDelay = max(maxDelay, d)
		}
	}
	return maxDelay, delayed
}

// With the linger off, and with it on but frames further apart than half the
// linger, every frame is written by its own push: no delay, no timer.
func TestSendQueue_NoLingerOnSlowStream(t *testing.T) {
	for _, tc := range []struct {
		name         string
		linger, tick time.Duration
	}{
		{"off, fast stream", 0, 10 * time.Microsecond},
		{"on, frames one linger apart", 200 * time.Microsecond, 200 * time.Microsecond},
		{"on, idle stream", 200 * time.Microsecond, 50 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				q, l := newLingerQueue(tc.linger)
				for i := 0; i < 500; i++ {
					l.push(t, q, i)
					if got := len(l.order); got != i+1 { // written before push returned
						t.Fatalf("frame %d not written by its own push (%d written)", i, got)
					}
					time.Sleep(tc.tick)
				}
				if maxDelay, delayed := l.check(t, 500); delayed != 0 {
					t.Fatalf("%d frames delayed, by up to %v", delayed, maxDelay)
				}
				if q.timer != nil {
					t.Fatal("a timer was armed")
				}
			})
		})
	}
}

// On a busy stream the linger gathers frames into batches and no frame waits
// longer than the linger. When the stream goes quiet the wait switches off
// again with the first slow frame.
func TestSendQueue_LingerBatchesBusyStreamWithinBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const linger = 200 * time.Microsecond
		q, l := newLingerQueue(linger)
		n := 0
		for ; n < 1000; n++ { // 50,000 frames/s
			l.push(t, q, n)
			time.Sleep(20 * time.Microsecond)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		maxDelay, delayed := l.check(t, n)
		if maxDelay > linger {
			t.Fatalf("a frame waited %v, above the %v linger", maxDelay, linger)
		}
		if delayed < n*8/10 {
			t.Fatalf("only %d of %d frames lingered on a busy stream", delayed, n)
		}
		if mean := float64(n) / float64(len(l.batches)); mean < 5 {
			t.Fatalf("mean batch %.1f frames, want about 10 (linger / interval)", mean)
		}

		// Quiet again: the first frame after the pause is written at once.
		for ; n < 1010; n++ {
			l.push(t, q, n)
			if got := len(l.order); got != n+1 {
				t.Fatalf("frame %d after a pause was delayed", n)
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
}

// A short burst on a quiet stream does not switch the wait on: it takes a few
// dozen fast frames in a row.
func TestSendQueue_LingerIgnoresShortBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q, l := newLingerQueue(200 * time.Microsecond)
		n := 0
		for round := 0; round < 50; round++ {
			for k := 0; k < 10; k++ { // ten frames 10 us apart, then a pause
				l.push(t, q, n)
				n++
				time.Sleep(10 * time.Microsecond)
			}
			time.Sleep(5 * time.Millisecond)
		}
		if _, delayed := l.check(t, n); delayed != 0 {
			t.Fatalf("%d frames of short bursts were delayed", delayed)
		}
	})
}

// warm makes q consider its stream busy, so the next idle push starts a wait.
func warm(q *sendQueue[int]) {
	q.arrived, q.gap = time.Now(), 0
}

// A wait ends as soon as the gathered frames fill a batch, by count or by size.
func TestSendQueue_LingerEndsWhenBatchFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q, l := newLingerQueue(MaxWriteLinger)
		warm(q)
		for i := 0; i < maxBatchFrames; i++ {
			l.push(t, q, i)
		}
		if maxDelay, _ := l.check(t, maxBatchFrames); maxDelay != 0 { // no fake time passed
			t.Fatalf("full batch waited %v", maxDelay)
		}
		if len(l.batches) != 1 || l.batches[0] != maxBatchFrames {
			t.Fatalf("batches %v, want one of %d", l.batches, maxBatchFrames)
		}

		q, l = newLingerQueue(MaxWriteLinger)
		q.size = func(int) int { return maxBatchBytes / 4 }
		warm(q)
		for i := 0; i < 4; i++ {
			l.push(t, q, i)
		}
		if maxDelay, _ := l.check(t, 4); maxDelay != 0 {
			t.Fatalf("batch at the byte cap waited %v", maxDelay)
		}
		time.Sleep(time.Second) // the stopped timer must not write again
		synctest.Wait()
		l.check(t, 4)
	})
}

// B4 with a linger: a frame accepted into a wait returns nil (it was queued,
// not written); when the write at the end of the wait fails nobody is told
// "not written" after the fact, the stream is broken for every later push. The
// goroutine that fills a batch writes it itself and does get the error: its own
// frame is in the failed write.
func TestSendQueue_LingerErrorAttribution(t *testing.T) {
	boom := errors.New("boom")
	synctest.Test(t, func(t *testing.T) {
		q := &sendQueue[int]{
			write:  func(int) error { return boom },
			batch:  func([]int) error { return boom },
			linger: 200 * time.Microsecond,
		}
		warm(q)
		for i := 0; i < 3; i++ {
			if err := q.push(i); err != nil {
				t.Fatalf("push %d into a wait: %v", i, err)
			}
		}
		time.Sleep(time.Millisecond) // the wait ends, the write fails
		synctest.Wait()
		if err := q.push(3); !errors.Is(err, boom) {
			t.Fatalf("stream must be broken after the failed write: %v", err)
		}

		q = &sendQueue[int]{
			write:  func(int) error { return boom },
			batch:  func([]int) error { return boom },
			linger: MaxWriteLinger,
		}
		warm(q)
		for i := 0; i < maxBatchFrames-1; i++ {
			if err := q.push(i); err != nil {
				t.Fatalf("push %d into a wait: %v", i, err)
			}
		}
		if err := q.push(maxBatchFrames - 1); !errors.Is(err, boom) {
			t.Fatalf("the push that fills and writes the batch must see its failure: %v", err)
		}
	})
}

// The owner of a write that succeeded is not blamed for a later failed wait.
func TestSendQueue_LingerForeignErrorNotAttributed(t *testing.T) {
	boom := errors.New("boom")
	synctest.Test(t, func(t *testing.T) {
		q := &sendQueue[int]{
			write:  func(i int) error { return nil },
			batch:  func([]int) error { return boom },
			linger: 200 * time.Microsecond,
		}
		if err := q.push(0); err != nil { // quiet stream: written at once
			t.Fatal(err)
		}
		warm(q)
		for i := 1; i < 4; i++ {
			if err := q.push(i); err != nil {
				t.Fatalf("push %d: %v", i, err)
			}
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		q.mu.Lock()
		written, err := q.written, q.err
		q.mu.Unlock()
		if written != 1 || !errors.Is(err, boom) {
			t.Fatalf("written %d err %v, want 1 frame written and the stream broken", written, err)
		}
	})
}

func TestClampLinger(t *testing.T) {
	for in, want := range map[time.Duration]time.Duration{
		-time.Second: 0, 0: 0, 200 * time.Microsecond: 200 * time.Microsecond, time.Second: MaxWriteLinger,
	} {
		if got := clampLinger(in); got != want {
			t.Errorf("clampLinger(%v) = %v, want %v", in, got, want)
		}
	}
}
