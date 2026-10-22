package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/vaibenz/pricestream/internal/hub"
	"github.com/vaibenz/pricestream/internal/quote"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func startServer(t *testing.T, cfg Config) (*hub.Hub, *Server, *httptest.Server) {
	t.Helper()
	h := hub.New()
	s := New(h, cfg, quiet)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return h, s, srv
}

func dial(t *testing.T, srv *httptest.Server, query string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws" + query
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func read(t *testing.T, conn *websocket.Conn) serverMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m serverMsg
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return m
}

func write(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	if err := conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func q(sym string, p float64) quote.Quote {
	return quote.Quote{Symbol: sym, Price: p, IngestTime: time.Now()}
}

func TestStreamsSubscribedPrices(t *testing.T) {
	h, _, srv := startServer(t, Config{})
	h.Publish(q("BTC-USD", 60000))

	conn := dial(t, srv, "?symbols=btc-usd")
	if m := read(t, conn); m.Type != "subscribed" || len(m.Symbols) != 1 || m.Symbols[0] != "BTC-USD" {
		t.Fatalf("ack = %+v", m)
	}
	// The latest known price arrives straight away.
	if m := read(t, conn); m.Type != "prices" || m.Data[0].Price != 60000 {
		t.Fatalf("snapshot = %+v", m)
	}

	h.Publish(q("ETH-USD", 3000)) // not subscribed
	h.Publish(q("BTC-USD", 60001))
	if m := read(t, conn); len(m.Data) != 1 || m.Data[0].Price != 60001 {
		t.Fatalf("update = %+v", m)
	}
}

func TestSubscribeAndUnsubscribeMessages(t *testing.T) {
	h, _, srv := startServer(t, Config{})
	conn := dial(t, srv, "")
	read(t, conn) // empty ack

	write(t, conn, clientMsg{Type: "subscribe", Symbols: []string{"ETH-USD", "SOL-USD"}})
	if m := read(t, conn); m.Type != "subscribed" || strings.Join(m.Symbols, ",") != "ETH-USD,SOL-USD" {
		t.Fatalf("ack = %+v", m)
	}
	write(t, conn, clientMsg{Type: "unsubscribe", Symbols: []string{"SOL-USD"}})
	if m := read(t, conn); strings.Join(m.Symbols, ",") != "ETH-USD" {
		t.Fatalf("ack = %+v", m)
	}
	h.Publish(q("SOL-USD", 150))
	h.Publish(q("ETH-USD", 3000))
	if m := read(t, conn); len(m.Data) != 1 || m.Data[0].Symbol != "ETH-USD" {
		t.Fatalf("update = %+v", m)
	}
}

func TestRejectsBadInput(t *testing.T) {
	_, _, srv := startServer(t, Config{MaxSymbols: 2})

	resp, err := http.Get(srv.URL + "/ws?symbols=not_a_symbol")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad symbol in query: %d", resp.StatusCode)
	}

	conn := dial(t, srv, "")
	read(t, conn)
	for _, msg := range []any{
		"not json",
		clientMsg{Type: "subscribe", Symbols: []string{"BTC"}},
		clientMsg{Type: "subscribe", Symbols: []string{"A-USD", "B1-USD", "C1-USD"}},
		clientMsg{Type: "dance", Symbols: []string{"BTC-USD"}},
	} {
		if s, ok := msg.(string); ok {
			conn.Write(context.Background(), websocket.MessageText, []byte(s))
		} else {
			write(t, conn, msg)
		}
		if m := read(t, conn); m.Type != "error" {
			t.Errorf("%v: got %+v", msg, m)
		}
	}
}

// With a minimum frame interval, a burst between frames reaches the client
// as one frame carrying only the newest price.
func TestMinIntervalCoalescesBursts(t *testing.T) {
	h, s, srv := startServer(t, Config{MinInterval: 200 * time.Millisecond})
	conn := dial(t, srv, "?symbols=BTC-USD")
	read(t, conn)

	h.Publish(q("BTC-USD", 1))
	if m := read(t, conn); m.Data[0].Price != 1 {
		t.Fatalf("first = %+v", m)
	}
	for i := 2; i <= 500; i++ {
		h.Publish(q("BTC-USD", float64(i)))
	}
	m := read(t, conn)
	if len(m.Data) != 1 || m.Data[0].Price != 500 {
		t.Fatalf("after burst = %+v", m)
	}
	if got := s.framesSent.Load(); got != 2 {
		t.Errorf("frames sent = %d, want 2", got)
	}
}

func TestPricesAndMetricsEndpoints(t *testing.T) {
	h, _, srv := startServer(t, Config{})
	h.Publish(q("BTC-USD", 1))
	h.Publish(q("ETH-USD", 2))

	var prices struct{ Data []quote.Quote }
	getJSON(t, srv.URL+"/v1/prices?symbols=ETH-USD", &prices)
	if len(prices.Data) != 1 || prices.Data[0].Price != 2 {
		t.Errorf("prices = %+v", prices)
	}

	var m map[string]any
	getJSON(t, srv.URL+"/metrics", &m)
	if m["hub"].(map[string]any)["published"].(float64) != 2 {
		t.Errorf("metrics = %v", m)
	}
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}
