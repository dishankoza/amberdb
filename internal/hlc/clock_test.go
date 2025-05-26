package hlc_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dishankoza/amberdb/internal/hlc"
)

func TestNowMonotonic(t *testing.T) {
	clk := hlc.NewClock()
	prev := clk.Now()
	for i := 0; i < 1000; i++ {
		ts := clk.Now()
		if strings.Compare(ts, prev) < 0 {
			t.Fatalf("timestamp not monotonic: got %s < %s", ts, prev)
		}
		prev = ts
	}
}

func TestLogicalIncrement(t *testing.T) {
	clk := hlc.NewClock()
	// Freeze physical by mocking time, but here simulate same physical by quick calls
	first := clk.Now()
	// Immediately call Now again without waiting
	second := clk.Now()
	if first == second {
		t.Fatalf("expected logical increment, got identical timestamps")
	}
	// Check that prefix (physical part) same
	if first[:19] != second[:19] {
		t.Errorf("physical part changed: %s vs %s", first[:19], second[:19])
	}
}

func TestFormatWidth(t *testing.T) {
	clk := hlc.NewClock()
	ts := clk.Now()
	if len(ts) != 24 {
		t.Errorf("expected timestamp length 24, got %d", len(ts))
	}
}

func TestNowAdvancesPhysical(t *testing.T) {
	clk := hlc.NewClock()
	first := clk.Now()
	// Sleep to advance physical time
	time.Sleep(1 * time.Millisecond)
	second := clk.Now()
	if second[:19] == first[:19] {
		t.Errorf("expected physical advance, but physical parts equal: %s", first[:19])
	}
}
