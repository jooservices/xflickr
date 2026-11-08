package process

import "sync/atomic"

// ByteCounter tracks HTTP bytes attributed to this process.
// /proc/self/net/dev is namespace-wide, so collectors use this counter instead.
type ByteCounter struct {
	in  atomic.Uint64
	out atomic.Uint64
}

// AddIn records received bytes.
func (c *ByteCounter) AddIn(n uint64) { c.in.Add(n) }

// AddOut records sent bytes.
func (c *ByteCounter) AddOut(n uint64) { c.out.Add(n) }

// Snapshot returns cumulative in/out byte counts.
func (c *ByteCounter) Snapshot() (in, out uint64) {
	return c.in.Load(), c.out.Load()
}
