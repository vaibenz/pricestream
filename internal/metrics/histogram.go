// Package metrics has a small lock free latency histogram.
package metrics

import (
	"math"
	"sync/atomic"
	"time"
)

// Bucket upper bounds, roughly three per decade from 10µs to 10s. Quantiles
// are reported as the upper bound of the bucket they land in, so they are
// conservative by at most one bucket width.
var bounds = func() []time.Duration {
	var b []time.Duration
	for d := 10 * time.Microsecond; d <= 10*time.Second; d *= 10 {
		b = append(b, d, 2*d, 5*d)
	}
	return b
}()

type Histogram struct {
	counts [32]atomic.Int64 // len(bounds)+1 overflow bucket
	total  atomic.Int64
	sumNs  atomic.Int64
}

func (h *Histogram) Observe(d time.Duration) {
	i := 0
	for i < len(bounds) && d > bounds[i] {
		i++
	}
	h.counts[i].Add(1)
	h.total.Add(1)
	h.sumNs.Add(int64(d))
}

// Quantile returns an upper bound for the p quantile (0 < p <= 1).
func (h *Histogram) Quantile(p float64) time.Duration {
	n := h.total.Load()
	if n == 0 {
		return 0
	}
	rank := int64(math.Ceil(p * float64(n)))
	var seen int64
	for i := range bounds {
		seen += h.counts[i].Load()
		if seen >= rank {
			return bounds[i]
		}
	}
	return time.Duration(math.MaxInt64)
}

type Summary struct {
	Count  int64   `json:"count"`
	MeanMs float64 `json:"mean_ms"`
	P50Ms  float64 `json:"p50_ms"`
	P90Ms  float64 `json:"p90_ms"`
	P99Ms  float64 `json:"p99_ms"`
}

func (h *Histogram) Summary() Summary {
	n := h.total.Load()
	s := Summary{Count: n}
	if n == 0 {
		return s
	}
	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	s.MeanMs = ms(time.Duration(h.sumNs.Load() / n))
	s.P50Ms, s.P90Ms, s.P99Ms = ms(h.Quantile(0.5)), ms(h.Quantile(0.9)), ms(h.Quantile(0.99))
	return s
}
