package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/vaibenz/pricestream/internal/quote"
)

// DefaultCoinbaseURL is Coinbase Exchange's public market data feed. The
// ticker and heartbeat channels need no authentication.
const DefaultCoinbaseURL = "wss://ws-feed.exchange.coinbase.com"

// Coinbase streams the ticker channel for a set of products.
//
// It reconnects with jittered exponential backoff, and subscribes to the
// heartbeat channel so a connection that is open but silent (a stalled
// feed) is detected by a read deadline and replaced.
type Coinbase struct {
	URL      string
	Products []string
	// StallTimeout is how long to wait for any message, heartbeats
	// included, before reconnecting. Heartbeats arrive every second.
	StallTimeout time.Duration
	Log          *slog.Logger
}

func (c Coinbase) Run(ctx context.Context, out chan<- quote.Quote) error {
	if len(c.Products) == 0 {
		return errors.New("coinbase: no products configured")
	}
	if c.URL == "" {
		c.URL = DefaultCoinbaseURL
	}
	if c.StallTimeout == 0 {
		c.StallTimeout = 10 * time.Second
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}

	b := backoff{min: 250 * time.Millisecond, max: 30 * time.Second}
	for {
		start := time.Now()
		err := c.session(ctx, out)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A session that stayed up for a while was healthy, so the next
		// failure starts from the shortest delay again.
		if time.Since(start) > time.Minute {
			b.reset()
		}
		delay := b.next()
		c.Log.Warn("coinbase feed disconnected", "err", err, "retry_in", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

func (c Coinbase) session(ctx context.Context, out chan<- quote.Quote) error {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, _, err := websocket.Dial(dialCtx, c.URL, nil)
	cancel()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	sub, _ := json.Marshal(map[string]any{
		"type":        "subscribe",
		"product_ids": c.Products,
		"channels":    []string{"ticker", "heartbeat"},
	})
	if err := conn.Write(ctx, websocket.MessageText, sub); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	c.Log.Info("coinbase feed connected", "products", c.Products)

	for {
		readCtx, cancel := context.WithTimeout(ctx, c.StallTimeout)
		_, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("feed stalled: no message for %s", c.StallTimeout)
			}
			return err
		}
		q, ok, err := parseCoinbase(data, time.Now())
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		select {
		case out <- q:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

type coinbaseMsg struct {
	Type      string `json:"type"`
	ProductID string `json:"product_id"`
	Price     string `json:"price"`
	BestBid   string `json:"best_bid"`
	BestAsk   string `json:"best_ask"`
	Open24h   string `json:"open_24h"`
	Volume24h string `json:"volume_24h"`
	Time      string `json:"time"`
	Message   string `json:"message"`
	Reason    string `json:"reason"`
}

// parseCoinbase turns one feed message into a quote. ok is false for
// messages that carry no price (heartbeats, subscription acks). An error
// message from the exchange ends the session.
func parseCoinbase(data []byte, received time.Time) (q quote.Quote, ok bool, err error) {
	var m coinbaseMsg
	if err := json.Unmarshal(data, &m); err != nil {
		return q, false, fmt.Errorf("decode: %w", err)
	}
	switch m.Type {
	case "ticker":
	case "error":
		return q, false, fmt.Errorf("exchange error: %s %s", m.Message, m.Reason)
	default:
		return q, false, nil
	}

	price, err := strconv.ParseFloat(m.Price, 64)
	if err != nil || price <= 0 || m.ProductID == "" {
		// Skip a malformed tick rather than dropping the connection.
		return q, false, nil
	}
	q = quote.Quote{
		Symbol:     m.ProductID,
		Price:      price,
		Bid:        parseOr0(m.BestBid),
		Ask:        parseOr0(m.BestAsk),
		Open24h:    parseOr0(m.Open24h),
		Volume24h:  parseOr0(m.Volume24h),
		Source:     "coinbase",
		IngestTime: received,
	}
	if t, err := time.Parse(time.RFC3339Nano, m.Time); err == nil {
		q.ExchangeTime = t
	} else {
		q.ExchangeTime = received
	}
	return q, true, nil
}

func parseOr0(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// backoff is capped exponential backoff with full jitter, so a fleet of
// clients does not reconnect in lockstep after an outage.
type backoff struct {
	min, max time.Duration
	attempt  int
}

func (b *backoff) next() time.Duration {
	ceiling := b.min << min(b.attempt, 20)
	if ceiling > b.max || ceiling <= 0 {
		ceiling = b.max
	}
	b.attempt++
	return b.min/2 + time.Duration(rand.Int64N(int64(ceiling)))
}

func (b *backoff) reset() { b.attempt = 0 }
