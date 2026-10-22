package source

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/vaibenz/pricestream/internal/quote"
)

// TestLiveCoinbaseFeed connects to the real Coinbase Exchange feed. It only
// runs with LIVE_COINBASE=1 (see .github/workflows/live-feed.yml), so the
// normal suite never depends on the network.
func TestLiveCoinbaseFeed(t *testing.T) {
	if os.Getenv("LIVE_COINBASE") != "1" {
		t.Skip("set LIVE_COINBASE=1 to test against the real feed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	products := []string{"BTC-USD", "ETH-USD", "SOL-USD"}
	out := make(chan quote.Quote, 256)
	go Coinbase{
		Products: products,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}.Run(ctx, out)

	seen := map[string]quote.Quote{}
	for len(seen) < len(products) {
		select {
		case q := <-out:
			if q.Price <= 0 || q.Source != "coinbase" {
				t.Fatalf("implausible quote %+v", q)
			}
			if lag := q.IngestTime.Sub(q.ExchangeTime); lag > 10*time.Second || lag < -10*time.Second {
				t.Errorf("%s exchange time is %v away from receipt", q.Symbol, lag)
			}
			seen[q.Symbol] = q
		case <-ctx.Done():
			t.Fatalf("only received %d of %d symbols: %v", len(seen), len(products), seen)
		}
	}
	for _, p := range products {
		q := seen[p]
		t.Logf("%s %.2f (bid %.2f ask %.2f, 24h open %.2f)", q.Symbol, q.Price, q.Bid, q.Ask, q.Open24h)
	}
}
