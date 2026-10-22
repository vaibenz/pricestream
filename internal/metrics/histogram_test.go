package metrics

import (
	"testing"
	"time"
)

func TestQuantiles(t *testing.T) {
	var h Histogram
	for i := 0; i < 90; i++ {
		h.Observe(140 * time.Microsecond) // lands in the 150µs bucket
	}
	for i := 0; i < 9; i++ {
		h.Observe(3 * time.Millisecond) // exactly on the 3ms bound
	}
	h.Observe(40 * time.Millisecond) // exactly on the 40ms bound

	cases := map[float64]time.Duration{
		0.5:  150 * time.Microsecond,
		0.9:  150 * time.Microsecond,
		0.95: 3 * time.Millisecond,
		0.99: 3 * time.Millisecond,
		1.0:  40 * time.Millisecond,
	}
	for p, want := range cases {
		if got := h.Quantile(p); got != want {
			t.Errorf("p%v = %v, want %v", p*100, got, want)
		}
	}
	if s := h.Summary(); s.Count != 100 || s.P99Ms != 3 {
		t.Errorf("summary %+v", s)
	}
}

func TestEmptyAndOverflow(t *testing.T) {
	var h Histogram
	if h.Quantile(0.5) != 0 || h.Summary().Count != 0 {
		t.Error("empty histogram should report zero")
	}
	h.Observe(time.Hour)
	if h.Quantile(0.5) < time.Hour {
		t.Error("overflow bucket should report an unbounded quantile")
	}
}
