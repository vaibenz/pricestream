// Package bus carries quotes from ingestors to gateways over Redis.
//
// Two structures are used: a pub/sub channel for the live stream, and a hash
// holding the latest quote per symbol. Pub/sub drops messages for anyone not
// listening, so a gateway that starts or reconnects reads the hash to get a
// full snapshot instead of waiting for every symbol to tick again.
package bus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/vaibenz/pricestream/internal/quote"
)

type Bus struct {
	rdb     *redis.Client
	channel string
	latest  string
}

// New uses keys under prefix, so several environments can share a Redis.
func New(rdb *redis.Client, prefix string) *Bus {
	return &Bus{rdb: rdb, channel: prefix + ":stream", latest: prefix + ":latest"}
}

// Connect parses a redis:// URL and checks the server is reachable.
func Connect(ctx context.Context, url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opts)
	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		return nil, err
	}
	return rdb, nil
}

// Publish stores q as the latest for its symbol and broadcasts it, in one
// round trip.
func (b *Bus) Publish(ctx context.Context, q quote.Quote) error {
	data, err := json.Marshal(q)
	if err != nil {
		return err
	}
	pipe := b.rdb.Pipeline()
	pipe.HSet(ctx, b.latest, q.Symbol, data)
	pipe.Publish(ctx, b.channel, data)
	_, err = pipe.Exec(ctx)
	return err
}

// Snapshot returns the latest quote for every symbol.
func (b *Bus) Snapshot(ctx context.Context) (map[string]quote.Quote, error) {
	raw, err := b.rdb.HGetAll(ctx, b.latest).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]quote.Quote, len(raw))
	for sym, data := range raw {
		var q quote.Quote
		if err := json.Unmarshal([]byte(data), &q); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", sym, err)
		}
		out[sym] = q
	}
	return out, nil
}

// Subscribe delivers live quotes to fn until ctx is cancelled. The Redis
// client resubscribes by itself if the connection drops. ready, if not nil,
// is closed once the subscription is confirmed, so a caller can take a
// snapshot afterwards without missing anything in between.
func (b *Bus) Subscribe(ctx context.Context, ready chan<- struct{}, fn func(quote.Quote)) error {
	sub := b.rdb.Subscribe(ctx, b.channel)
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	if ready != nil {
		close(ready)
	}
	ch := sub.Channel(redis.WithChannelSize(4096))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return fmt.Errorf("subscription closed")
			}
			var q quote.Quote
			if err := json.Unmarshal([]byte(msg.Payload), &q); err != nil {
				continue
			}
			fn(q)
		}
	}
}
