// Command ingest reads a market data feed and publishes quotes to Redis.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vaibenz/pricestream/internal/bus"
	"github.com/vaibenz/pricestream/internal/quote"
	"github.com/vaibenz/pricestream/internal/source"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil && err != context.Canceled {
		log.Error("exit", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rdb, err := bus.Connect(ctx, envOr("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		return err
	}
	defer rdb.Close()
	b := bus.New(rdb, envOr("BUS_PREFIX", "prices"))

	products := strings.Split(envOr("PRODUCTS", "BTC-USD,ETH-USD,SOL-USD"), ",")
	var src source.Source
	switch envOr("SOURCE", "coinbase") {
	case "sim":
		start := map[string]float64{}
		for i, p := range products {
			start[p] = 100 * float64(i+1)
		}
		start["BTC-USD"], start["ETH-USD"], start["SOL-USD"] = 67_000, 2_600, 170
		interval, _ := time.ParseDuration(envOr("SIM_INTERVAL", "200ms"))
		src = source.Sim{Start: start, Interval: interval, Seed: uint64(time.Now().UnixNano())}
	default:
		src = source.Coinbase{URL: os.Getenv("COINBASE_URL"), Products: products, Log: log}
	}

	quotes := make(chan quote.Quote, 1024)
	errc := make(chan error, 1)
	go func() { errc <- src.Run(ctx, quotes) }()

	var published, failed int
	stats := time.NewTicker(30 * time.Second)
	defer stats.Stop()
	for {
		select {
		case err := <-errc:
			return err
		case <-stats.C:
			log.Info("ingest stats", "published", published, "failed", failed)
			published, failed = 0, 0
		case q := <-quotes:
			if err := b.Publish(ctx, q); err != nil {
				failed++
				log.Warn("publish failed", "symbol", q.Symbol, "err", err)
				continue
			}
			published++
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
