package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/vaibenz/pricestream/internal/bus"
	"github.com/vaibenz/pricestream/internal/hub"
	"github.com/vaibenz/pricestream/internal/quote"
)

// Pump feeds the hub from Redis until ctx is cancelled. Each (re)start
// subscribes first and only then loads the snapshot, so no update that
// lands in between is lost. The hub ignores anything older than what it
// already holds, so overlap between the two is harmless.
func Pump(ctx context.Context, b *bus.Bus, h *hub.Hub, log *slog.Logger) error {
	delay := 250 * time.Millisecond
	for {
		ready := make(chan struct{})
		errc := make(chan error, 1)
		go func() { errc <- b.Subscribe(ctx, ready, func(q quote.Quote) { h.Publish(q) }) }()

		select {
		case <-ready:
			if snap, err := b.Snapshot(ctx); err == nil {
				h.Load(snap)
				log.Info("loaded snapshot", "symbols", len(snap))
			} else {
				log.Warn("snapshot failed", "err", err)
			}
			delay = 250 * time.Millisecond
		case <-errc:
		}

		err := <-errc
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Warn("redis subscription ended", "err", err, "retry_in", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, 10*time.Second)
	}
}
