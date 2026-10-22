package gateway

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/vaibenz/pricestream/internal/bus"
)

// End to end: ingestor side publishes to Redis, the pump feeds the hub, and a
// WebSocket client sees both the snapshot and live updates.
func TestRedisToWebSocket(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rdb, err := bus.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Close()
	prefix := fmt.Sprintf("test-%d", rand.Uint64())
	defer rdb.Del(context.Background(), prefix+":latest")
	b := bus.New(rdb, prefix)

	// Published before the gateway starts: must come from the snapshot.
	if err := b.Publish(ctx, q("BTC-USD", 100)); err != nil {
		t.Fatal(err)
	}

	h, _, srv := startServer(t, Config{})
	go Pump(ctx, b, h, quiet)
	for len(h.Latest("BTC-USD")) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("snapshot never loaded")
		case <-time.After(10 * time.Millisecond):
		}
	}

	conn := dial(t, srv, "?symbols=BTC-USD")
	read(t, conn)
	if m := read(t, conn); m.Data[0].Price != 100 {
		t.Fatalf("snapshot = %+v", m)
	}
	if err := b.Publish(ctx, q("BTC-USD", 101)); err != nil {
		t.Fatal(err)
	}
	if m := read(t, conn); m.Data[0].Price != 101 {
		t.Fatalf("live = %+v", m)
	}
}
