package bus

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/vaibenz/pricestream/internal/quote"
)

func newBus(t *testing.T) *Bus {
	t.Helper()
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set; skipping integration test")
	}
	rdb, err := Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("test-%d", rand.Uint64())
	t.Cleanup(func() {
		rdb.Del(context.Background(), prefix+":latest")
		rdb.Close()
	})
	return New(rdb, prefix)
}

func TestPublishSubscribeAndSnapshot(t *testing.T) {
	b := newBus(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got := make(chan quote.Quote, 10)
	ready := make(chan struct{})
	go b.Subscribe(ctx, ready, func(q quote.Quote) { got <- q })
	<-ready

	for i, p := range []float64{100, 101, 102} {
		if err := b.Publish(ctx, quote.Quote{Symbol: "BTC-USD", Price: p, Source: "test"}); err != nil {
			t.Fatal(err)
		}
		select {
		case q := <-got:
			if q.Price != p {
				t.Errorf("message %d: price %v, want %v", i, q.Price, p)
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for message")
		}
	}
	b.Publish(ctx, quote.Quote{Symbol: "ETH-USD", Price: 3000})

	snap, err := b.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap) != 2 || snap["BTC-USD"].Price != 102 || snap["ETH-USD"].Price != 3000 {
		t.Errorf("snapshot = %+v", snap)
	}
}
