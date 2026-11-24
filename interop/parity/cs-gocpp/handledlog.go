package main

import (
	"bufio"
	"fmt"
	"os"
	"sync"
	"time"
)

// handledLog records which charger a backend handled and when, buffered (flushed
// every second), so a load test can reconstruct which pool served each charger.
type handledLog struct {
	mu sync.Mutex
	w  *bufio.Writer
}

func newHandledLog(path string) (*handledLog, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	l := &handledLog{w: bufio.NewWriterSize(f, 256<<10)}
	go func() {
		for range time.Tick(time.Second) {
			l.mu.Lock()
			_ = l.w.Flush()
			l.mu.Unlock()
		}
	}()
	return l, nil
}

// note is safe on a nil receiver (no log requested).
func (l *handledLog) note(cp, action string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	fmt.Fprintf(l.w, "%d %s %s\n", time.Now().UnixMilli(), cp, action)
	l.mu.Unlock()
}
