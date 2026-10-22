package source

import (
	"context"
	"math"
	"math/rand/v2"
	"time"

	"github.com/vaibenz/pricestream/internal/quote"
)

// Sim is a synthetic feed: a geometric random walk per symbol. It lets the
// whole stack run offline and drives load tests at rates a real feed won't.
type Sim struct {
	// Start maps each symbol to its starting price.
	Start map[string]float64
	// Interval is the mean time between ticks for each symbol.
	Interval time.Duration
	// Volatility is the per tick standard deviation of log returns.
	Volatility float64
	Seed       uint64
}

func (s Sim) Run(ctx context.Context, out chan<- quote.Quote) error {
	rng := rand.New(rand.NewPCG(s.Seed, s.Seed^0x9e3779b97f4a7c15))
	vol := s.Volatility
	if vol == 0 {
		vol = 0.0005
	}
	interval := s.Interval
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}

	type state struct{ price, open, volume float64 }
	states := make(map[string]*state, len(s.Start))
	symbols := make([]string, 0, len(s.Start))
	for sym, p := range s.Start {
		// Pretend the day opened a few percent away, so 24h change looks
		// like a real market rather than starting at exactly 0%.
		open := p * math.Exp(0.03*rng.NormFloat64())
		states[sym] = &state{price: p, open: round(open)}
		symbols = append(symbols, sym)
	}
	if len(symbols) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	// One timer drives all symbols: each fire ticks one random symbol, so
	// the aggregate rate is len(symbols)/Interval.
	mean := interval / time.Duration(len(symbols))
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		sym := symbols[rng.IntN(len(symbols))]
		st := states[sym]
		st.price *= math.Exp(vol * rng.NormFloat64())
		st.volume += rng.Float64() * 2
		spread := st.price * 0.0001
		now := time.Now()
		q := quote.Quote{
			Symbol: sym, Price: round(st.price), Bid: round(st.price - spread/2),
			Ask: round(st.price + spread/2), Open24h: st.open, Volume24h: st.volume,
			Source: "sim", ExchangeTime: now, IngestTime: now,
		}
		select {
		case out <- q:
		case <-ctx.Done():
			return ctx.Err()
		}
		// Exponential gaps make arrivals bursty, like a real feed.
		timer.Reset(time.Duration(rng.ExpFloat64() * float64(mean)))
	}
}

// round keeps four significant digits after the point for sub dollar coins.
func round(p float64) float64 {
	if p < 1 {
		return math.Round(p*1e6) / 1e6
	}
	return math.Round(p*100) / 100
}
