package source

import (
	"context"
	"testing"
	"time"

	"github.com/vaibenz/pricestream/internal/quote"
)

func TestSimProducesPlausibleQuotes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	out := make(chan quote.Quote, 1024)
	sim := Sim{Start: map[string]float64{"BTC-USD": 60_000, "ETH-USD": 3_000}, Interval: 5 * time.Millisecond, Seed: 1}
	go sim.Run(ctx, out)

	seen := map[string]int{}
	for len(seen) < 2 || seen["BTC-USD"]+seen["ETH-USD"] < 50 {
		select {
		case q := <-out:
			seen[q.Symbol]++
			if q.Price <= 0 || q.Bid >= q.Ask || q.Source != "sim" {
				t.Fatalf("implausible quote %+v", q)
			}
			if q.Symbol == "BTC-USD" && (q.Price < 30_000 || q.Price > 120_000) {
				t.Fatalf("BTC wandered too far: %v", q.Price)
			}
		case <-ctx.Done():
			t.Fatalf("only saw %v", seen)
		}
	}
}

func TestSimStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		done <- Sim{Start: map[string]float64{"X-USD": 1}}.Run(ctx, make(chan quote.Quote))
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
