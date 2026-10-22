package hub

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/vaibenz/pricestream/internal/quote"
)

func q(sym string, price float64) quote.Quote {
	return quote.Quote{Symbol: sym, Price: price, IngestTime: time.Now()}
}

func next(t *testing.T, c *Client) []quote.Quote {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	batch, err := c.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return batch
}

func noneReady(t *testing.T, c *Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if batch, err := c.Next(ctx); err == nil {
		t.Fatalf("expected nothing pending, got %+v", batch)
	}
}

func TestOnlySubscribedSymbolsAreDelivered(t *testing.T) {
	h := New()
	c := h.Register()
	h.Subscribe(c, "BTC-USD")

	h.Publish(q("ETH-USD", 3000))
	noneReady(t, c)

	h.Publish(q("BTC-USD", 60000))
	if b := next(t, c); len(b) != 1 || b[0].Price != 60000 {
		t.Errorf("got %+v", b)
	}
}

func TestBurstIsCoalescedToLatest(t *testing.T) {
	h := New()
	c := h.Register()
	h.Subscribe(c, "BTC-USD", "ETH-USD")

	for i := 0; i < 100; i++ {
		h.Publish(q("BTC-USD", float64(60000+i)))
	}
	h.Publish(q("ETH-USD", 3000))

	b := next(t, c)
	if len(b) != 2 || b[0].Symbol != "BTC-USD" || b[0].Price != 60099 || b[1].Symbol != "ETH-USD" {
		t.Errorf("got %+v", b)
	}
	if got := h.Stats().Coalesced; got != 99 {
		t.Errorf("coalesced = %d, want 99", got)
	}
	noneReady(t, c)
}

func TestNewSubscriberGetsLatestImmediately(t *testing.T) {
	h := New()
	h.Publish(q("BTC-USD", 1))
	h.Publish(q("BTC-USD", 2))

	c := h.Register()
	h.Subscribe(c, "BTC-USD", "DOGE-USD")
	if b := next(t, c); len(b) != 1 || b[0].Price != 2 {
		t.Errorf("got %+v", b)
	}
}

func TestUnsubscribeDropsPending(t *testing.T) {
	h := New()
	c := h.Register()
	h.Subscribe(c, "BTC-USD")
	h.Publish(q("BTC-USD", 1))
	h.Unsubscribe(c, "BTC-USD")
	noneReady(t, c)

	h.Publish(q("BTC-USD", 2))
	noneReady(t, c)
	if len(c.Symbols()) != 0 {
		t.Errorf("symbols = %v", c.Symbols())
	}
}

func TestOutOfOrderQuoteIsIgnored(t *testing.T) {
	h := New()
	now := time.Now()
	h.Publish(quote.Quote{Symbol: "BTC-USD", Price: 2, IngestTime: now})
	h.Publish(quote.Quote{Symbol: "BTC-USD", Price: 1, IngestTime: now.Add(-time.Second)})
	if l := h.Latest("BTC-USD"); l[0].Price != 2 {
		t.Errorf("latest = %+v", l)
	}
}

func TestUnregisterRemovesClient(t *testing.T) {
	h := New()
	c := h.Register()
	h.Subscribe(c, "BTC-USD")
	h.Unregister(c)
	if s := h.Stats(); s.Clients != 0 || len(h.bySymbol) != 0 {
		t.Errorf("stats %+v bySymbol %v", s, h.bySymbol)
	}
}

// A stalled client must not hold up the publisher or other clients.
func TestSlowClientDoesNotBlockOthers(t *testing.T) {
	h := New()
	slow := h.Register()
	fast := h.Register()
	h.Subscribe(slow, "BTC-USD")
	h.Subscribe(fast, "BTC-USD")

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100_000; i++ {
			h.Publish(q("BTC-USD", float64(i)))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publisher blocked by a client that never reads")
	}
	if b := next(t, fast); b[0].Price != 99_999 {
		t.Errorf("fast client got %+v", b)
	}
	if b := next(t, slow); b[0].Price != 99_999 {
		t.Errorf("slow client should get only the latest, got %+v", b)
	}
}

func TestConcurrentUse(t *testing.T) {
	h := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := h.Register()
			defer h.Unregister(c)
			h.Subscribe(c, "BTC-USD", fmt.Sprintf("S%d-USD", i%5))
			for j := 0; j < 50; j++ {
				if _, err := c.Next(ctx); err != nil {
					return
				}
				if j == 25 {
					h.Unsubscribe(c, "BTC-USD")
					h.Subscribe(c, "BTC-USD")
				}
			}
		}(i)
	}
	for k := 0; k < 4; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for j := 0; ctx.Err() == nil && j < 20_000; j++ {
				h.Publish(q(fmt.Sprintf("S%d-USD", j%5), float64(j)))
				h.Publish(q("BTC-USD", float64(j)))
			}
		}(k)
	}
	wg.Wait()
}
