package hlc

import (
	"fmt"
	"sync"
	"time"
)

// Clock implements a Hybrid Logical Clock (HLC).
type Clock struct {
	mu           sync.Mutex
	lastPhysical int64
	logical      uint32
}

// NewClock creates a new HLC clock.
func NewClock() *Clock {
	return &Clock{}
}

// Now returns a new HLC timestamp as a fixed-width string.
// Format: <19-digit physical nanoseconds><5-digit logical counter>
func (c *Clock) Now() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	phy := time.Now().UnixNano()
	const threshold = 1000000 // nanoseconds (1ms)
	if phy > c.lastPhysical {
		if phy-c.lastPhysical < threshold {
			// Treat small advances (under 1ms) as same physical
			c.logical++
		} else {
			c.lastPhysical = phy
			c.logical = 0
		}
	} else {
		c.logical++
	}
	// fixed width: 19 digits physical + 5 digits logical
	return fmt.Sprintf("%019d%05d", c.lastPhysical, c.logical)
}
