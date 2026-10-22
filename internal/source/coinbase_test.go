package source

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/vaibenz/pricestream/internal/quote"
)

const tickerJSON = `{"type":"ticker","sequence":1,"product_id":"BTC-USD","price":"67012.34",
 "open_24h":"66000.00","volume_24h":"12345.6","low_24h":"65000","high_24h":"68000",
 "best_bid":"67012.33","best_ask":"67012.35","side":"buy","time":"2026-10-22T12:00:00.123456Z",
 "trade_id":42,"last_size":"0.01"}`

func TestParseTicker(t *testing.T) {
	now := time.Now()
	q, ok, err := parseCoinbase([]byte(tickerJSON), now)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if q.Symbol != "BTC-USD" || q.Price != 67012.34 || q.Bid != 67012.33 || q.Ask != 67012.35 {
		t.Errorf("parsed %+v", q)
	}
	if q.Open24h != 66000 || q.Volume24h != 12345.6 || q.Source != "coinbase" {
		t.Errorf("parsed %+v", q)
	}
	if !q.IngestTime.Equal(now) || q.ExchangeTime.Nanosecond() != 123456000 {
		t.Errorf("times %v %v", q.IngestTime, q.ExchangeTime)
	}
}

func TestParseNonTickerMessages(t *testing.T) {
	for _, msg := range []string{
		`{"type":"heartbeat","product_id":"BTC-USD","sequence":1}`,
		`{"type":"subscriptions","channels":[]}`,
		`{"type":"ticker","product_id":"BTC-USD","price":"not a number"}`,
		`{"type":"ticker","product_id":"","price":"1"}`,
	} {
		if _, ok, err := parseCoinbase([]byte(msg), time.Now()); ok || err != nil {
			t.Errorf("%s: ok=%v err=%v", msg, ok, err)
		}
	}
	if _, _, err := parseCoinbase([]byte(`{"type":"error","message":"Failed to subscribe","reason":"bad product"}`), time.Now()); err == nil {
		t.Error("error message should end the session")
	}
	if _, _, err := parseCoinbase([]byte(`{`), time.Now()); err == nil {
		t.Error("invalid JSON should be an error")
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	b := backoff{min: 100 * time.Millisecond, max: time.Second}
	for i := 0; i < 50; i++ {
		d := b.next()
		if d < 50*time.Millisecond || d > 1050*time.Millisecond {
			t.Fatalf("attempt %d: delay %v out of range", i, d)
		}
	}
	b.reset()
	if b.attempt != 0 {
		t.Error("reset did not clear attempts")
	}
}

// fakeFeed accepts WebSocket connections, checks the subscribe message and
// sends a few ticks. Its first connection then drops, its second goes silent
// to exercise stall detection, and later ones stay healthy.
func fakeFeed(t *testing.T, conns *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		n := conns.Add(1)
		ctx := r.Context()

		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var sub map[string]any
		json.Unmarshal(data, &sub)
		if sub["type"] != "subscribe" {
			t.Errorf("first message was %s", data)
			return
		}
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"subscriptions"}`))

		switch n {
		case 1:
			c.Write(ctx, websocket.MessageText, []byte(tickerJSON))
			c.Close(websocket.StatusGoingAway, "bye") // dropped connection
		case 2:
			<-ctx.Done() // open but silent
		default:
			for i := 0; ; i++ {
				msg := strings.Replace(tickerJSON, `"67012.34"`, `"`+strings.Repeat("1", 1+i%3)+`.5"`, 1)
				if c.Write(ctx, websocket.MessageText, []byte(msg)) != nil {
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}))
}

func TestCoinbaseReconnectsAfterDropAndStall(t *testing.T) {
	var conns atomic.Int32
	srv := fakeFeed(t, &conns)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out := make(chan quote.Quote, 100)
	cb := Coinbase{
		URL:          "ws" + strings.TrimPrefix(srv.URL, "http"),
		Products:     []string{"BTC-USD"},
		StallTimeout: 300 * time.Millisecond,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	done := make(chan error, 1)
	go func() { done <- cb.Run(ctx, out) }()

	got := 0
	for got < 10 {
		select {
		case q := <-out:
			if q.Symbol != "BTC-USD" {
				t.Fatalf("unexpected %+v", q)
			}
			got++
		case <-ctx.Done():
			t.Fatalf("got %d quotes over %d connections", got, conns.Load())
		}
	}
	if conns.Load() < 3 {
		t.Errorf("expected a reconnect after the drop and after the stall, saw %d connections", conns.Load())
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Errorf("Run returned %v", err)
	}
}
