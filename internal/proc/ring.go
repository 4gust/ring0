package proc

import "sync"

// RingBuffer holds the most recent N log lines for an app.
type RingBuffer struct {
	mu    sync.RWMutex
	data  []LogLine
	size  int
	start int
	count int
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{data: make([]LogLine, size), size: size}
}

func (r *RingBuffer) Push(l LogLine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := (r.start + r.count) % r.size
	r.data[idx] = l
	if r.count < r.size {
		r.count++
	} else {
		r.start = (r.start + 1) % r.size
	}
}

// Snapshot returns a copy in chronological order.
func (r *RingBuffer) Snapshot() []LogLine {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]LogLine, r.count)
	for i := 0; i < r.count; i++ {
		out[i] = r.data[(r.start+i)%r.size]
	}
	return out
}

func (r *RingBuffer) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}
