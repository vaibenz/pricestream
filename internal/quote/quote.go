// Package quote defines the price update that flows from exchange feeds,
// through Redis, out to clients.
package quote

import "time"

// Quote is the latest ticker state for one market. Keys are short because
// every client receives a stream of these.
//
// Prices are float64 because this is display data. Anything that settles
// money should carry exact decimals instead.
type Quote struct {
	Symbol    string  `json:"s"`
	Price     float64 `json:"p"`
	Bid       float64 `json:"b,omitempty"`
	Ask       float64 `json:"a,omitempty"`
	Open24h   float64 `json:"o,omitempty"`
	Volume24h float64 `json:"v,omitempty"`
	Source    string  `json:"src"`
	// ExchangeTime is when the exchange produced the tick. IngestTime is
	// when our ingestor received it, and is what latency is measured from.
	ExchangeTime time.Time `json:"et"`
	IngestTime   time.Time `json:"it"`
}

// Change24h is the fractional move since the 24h open, or 0 if unknown.
func (q Quote) Change24h() float64 {
	if q.Open24h == 0 {
		return 0
	}
	return (q.Price - q.Open24h) / q.Open24h
}
