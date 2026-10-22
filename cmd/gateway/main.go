// Command gateway streams quotes from Redis to WebSocket clients.
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vaibenz/pricestream/internal/bus"
	"github.com/vaibenz/pricestream/internal/gateway"
	"github.com/vaibenz/pricestream/internal/hub"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
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

	h := hub.New()
	go gateway.Pump(ctx, bus.New(rdb, envOr("BUS_PREFIX", "prices")), h, log)

	minInterval, _ := time.ParseDuration(envOr("MIN_INTERVAL", "100ms"))
	var origins []string
	if o := os.Getenv("ALLOWED_ORIGINS"); o != "" {
		origins = strings.Split(o, ",")
	}
	gw := gateway.New(h, gateway.Config{MinInterval: minInterval, OriginPatterns: origins}, log)

	srv := &http.Server{
		Addr:              ":" + envOr("PORT", "8080"),
		Handler:           gw.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		// Open WebSockets are hijacked, so Shutdown does not wait for them.
		// Deriving request contexts from ctx closes them on SIGTERM.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
