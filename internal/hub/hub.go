// Package hub fans quotes out to many subscribers with per client coalescing.
//
// Each client holds at most one pending quote per symbol. When a new quote
// arrives before the client's writer has sent the previous one, it simply
// replaces it. A slow client therefore always receives the freshest price
// and its memory use is bounded by the number of symbols it follows, never
// by how far behind it is. Publishing never blocks on a client.
package hub

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/vaibenz/pricestream/internal/quote"
)

type Hub struct {
	mu       sync.RWMutex
	latest   map[string]quote.Quote
	bySymbol map[string]map[*Client]struct{}
	clients  map[*Client]struct{}

	published atomic.Int64
	coalesced atomic.Int64
}

func New() *Hub {
	return &Hub{
		latest:   map[string]quote.Quote{},
		bySymbol: map[string]map[*Client]struct{}{},
		clients:  map[*Client]struct{}{},
	}
}

// Client is one subscriber's view of the hub.
type Client struct {
	hub     *Hub
	symbols map[string]struct{} // guarded by hub.mu

	mu      sync.Mutex
	pending map[string]quote.Quote
	notify  chan struct{}
}

// Register adds a client with no subscriptions.
func (h *Hub) Register() *Client {
	c := &Client{
		hub:     h,
		symbols: map[string]struct{}{},
		pending: map[string]quote.Quote{},
		notify:  make(chan struct{}, 1),
	}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

// Unregister removes a client and all its subscriptions.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sym := range c.symbols {
		h.removeSub(sym, c)
	}
	delete(h.clients, c)
}

// Subscribe adds symbols to a client and queues the latest known quote for
// each, so a new subscriber sees prices immediately rather than waiting for
// the next tick.
func (h *Hub) Subscribe(c *Client, symbols ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sym := range symbols {
		if _, ok := c.symbols[sym]; ok {
			continue
		}
		c.symbols[sym] = struct{}{}
		subs := h.bySymbol[sym]
		if subs == nil {
			subs = map[*Client]struct{}{}
			h.bySymbol[sym] = subs
		}
		subs[c] = struct{}{}
		if q, ok := h.latest[sym]; ok {
			c.offer(q)
		}
	}
}

// Unsubscribe removes symbols from a client and drops anything pending for
// them.
func (h *Hub) Unsubscribe(c *Client, symbols ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sym := range symbols {
		if _, ok := c.symbols[sym]; !ok {
			continue
		}
		delete(c.symbols, sym)
		h.removeSub(sym, c)
		c.mu.Lock()
		delete(c.pending, sym)
		c.mu.Unlock()
	}
}

func (h *Hub) removeSub(sym string, c *Client) {
	if subs := h.bySymbol[sym]; subs != nil {
		delete(subs, c)
		if len(subs) == 0 {
			delete(h.bySymbol, sym)
		}
	}
}

// Publish records q as the latest for its symbol and offers it to every
// subscriber. It takes only short locks and never waits on a client.
func (h *Hub) Publish(q quote.Quote) {
	h.mu.Lock()
	if prev, ok := h.latest[q.Symbol]; ok && q.IngestTime.Before(prev.IngestTime) {
		// Out of order delivery, for example two ingestors racing. Keep the
		// newer quote.
		h.mu.Unlock()
		return
	}
	h.latest[q.Symbol] = q
	for c := range h.bySymbol[q.Symbol] {
		c.offer(q)
	}
	h.mu.Unlock()
	h.published.Add(1)
}

// Load replaces the latest quotes, for example from a Redis snapshot at
// startup, without notifying anyone.
func (h *Hub) Load(snapshot map[string]quote.Quote) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sym, q := range snapshot {
		if prev, ok := h.latest[sym]; !ok || q.IngestTime.After(prev.IngestTime) {
			h.latest[sym] = q
		}
	}
}

// Latest returns the latest quote for each requested symbol that has one,
// or for every symbol if none are given, sorted by symbol.
func (h *Hub) Latest(symbols ...string) []quote.Quote {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := []quote.Quote{}
	if len(symbols) == 0 {
		for _, q := range h.latest {
			out = append(out, q)
		}
	} else {
		for _, sym := range symbols {
			if q, ok := h.latest[sym]; ok {
				out = append(out, q)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out
}

type Stats struct {
	Clients   int   `json:"clients"`
	Symbols   int   `json:"symbols"`
	Published int64 `json:"published"`
	// Coalesced counts quotes replaced before a client's writer sent them.
	Coalesced int64 `json:"coalesced"`
}

func (h *Hub) Stats() Stats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return Stats{
		Clients: len(h.clients), Symbols: len(h.latest),
		Published: h.published.Load(), Coalesced: h.coalesced.Load(),
	}
}

func (c *Client) offer(q quote.Quote) {
	c.mu.Lock()
	if _, waiting := c.pending[q.Symbol]; waiting {
		c.hub.coalesced.Add(1)
	}
	c.pending[q.Symbol] = q
	c.mu.Unlock()
	select {
	case c.notify <- struct{}{}:
	default: // already signalled
	}
}

// Next blocks until at least one quote is pending, then returns every
// pending quote (one per symbol, sorted) and clears them.
func (c *Client) Next(ctx context.Context) ([]quote.Quote, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.notify:
		}
		c.mu.Lock()
		if len(c.pending) == 0 {
			// Notified, but an unsubscribe cleared the queue.
			c.mu.Unlock()
			continue
		}
		batch := make([]quote.Quote, 0, len(c.pending))
		for _, q := range c.pending {
			batch = append(batch, q)
		}
		clear(c.pending)
		c.mu.Unlock()
		sort.Slice(batch, func(i, j int) bool { return batch[i].Symbol < batch[j].Symbol })
		return batch, nil
	}
}

// Symbols returns the client's subscriptions, sorted.
func (c *Client) Symbols() []string {
	c.hub.mu.RLock()
	defer c.hub.mu.RUnlock()
	out := make([]string, 0, len(c.symbols))
	for s := range c.symbols {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
