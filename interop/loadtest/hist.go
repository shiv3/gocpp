package main

import (
	"math"
	"time"
)

// hist is a log-bucket latency histogram (12% bucket width, 100us..~1h).
type hist struct {
	n   int64
	b   [160]int32
	max time.Duration
}

const histBase = 100e3 // ns

var histLn = math.Log(1.12)

func (h *hist) add(d time.Duration) {
	i := 0
	if d > 100*time.Microsecond {
		i = int(math.Log(float64(d)/histBase) / histLn)
		if i < 0 {
			i = 0
		}
		if i > 159 {
			i = 159
		}
	}
	h.b[i]++
	h.n++
	if d > h.max {
		h.max = d
	}
}

func (h *hist) merge(o *hist) {
	for i := range h.b {
		h.b[i] += o.b[i]
	}
	h.n += o.n
	if o.max > h.max {
		h.max = o.max
	}
}

// pct returns the upper bound of the bucket holding quantile q.
func (h *hist) pct(q float64) time.Duration {
	if h.n == 0 {
		return 0
	}
	target := int64(math.Ceil(q * float64(h.n)))
	var c int64
	for i, v := range h.b {
		c += int64(v)
		if c >= target {
			d := time.Duration(histBase * math.Pow(1.12, float64(i+1)))
			if d > h.max {
				d = h.max
			}
			return d
		}
	}
	return h.max
}
