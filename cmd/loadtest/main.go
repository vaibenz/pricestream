// Command loadtest opens many WebSocket clients against a gateway and
// reports end to end latency, from the ingestor receiving a tick to a client
// decoding it. Run it on the same host as the ingestor so clocks agree.
//
//	go run ./cmd/loadtest -url ws://localhost:8080/ws -clients 1000 -duration 30s
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/vaibenz/pricestream/internal/metrics"
	"github.com/vaibenz/pricestream/internal/quote"
)

func main() {
	url := flag.String("url", "ws://localhost:8080/ws", "gateway WebSocket URL")
	clients := flag.Int("clients", 500, "concurrent clients")
	symbols := flag.String("symbols", "BTC-USD,ETH-USD,SOL-USD", "symbols each client follows")
	duration := flag.Duration("duration", 20*time.Second, "how long to measure")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *duration+10*time.Second)
	defer cancel()

	var (
		latency   metrics.Histogram
		frames    atomic.Int64
		quotes    atomic.Int64
		connected atomic.Int64
		failed    atomic.Int64
		measuring atomic.Bool
		wg        sync.WaitGroup
	)

	for i := 0; i < *clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dctx, dcancel := context.WithTimeout(ctx, 10*time.Second)
			conn, _, err := websocket.Dial(dctx, *url+"?symbols="+*symbols, nil)
			dcancel()
			if err != nil {
				failed.Add(1)
				return
			}
			defer conn.CloseNow()
			conn.SetReadLimit(1 << 20)
			connected.Add(1)
			for {
				_, data, err := conn.Read(ctx)
				if err != nil {
					return
				}
				now := time.Now()
				var m struct {
					Type string        `json:"type"`
					Data []quote.Quote `json:"data"`
				}
				if json.Unmarshal(data, &m) != nil || m.Type != "prices" || !measuring.Load() {
					continue
				}
				frames.Add(1)
				for _, q := range m.Data {
					quotes.Add(1)
					latency.Observe(now.Sub(q.IngestTime))
				}
			}
		}()
		// Stagger connects so the gateway is not hit by one giant burst.
		if i%100 == 99 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Let every client receive its snapshot before measuring.
	time.Sleep(2 * time.Second)
	fmt.Printf("connected %d clients (%d failed)\n", connected.Load(), failed.Load())
	measuring.Store(true)
	start := time.Now()
	time.Sleep(*duration)
	measuring.Store(false)
	elapsed := time.Since(start)
	cancel()
	wg.Wait()

	s := latency.Summary()
	fmt.Printf("frames received   %d (%.0f/s across all clients)\n", frames.Load(), float64(frames.Load())/elapsed.Seconds())
	fmt.Printf("quotes received   %d (%.0f/s)\n", quotes.Load(), float64(quotes.Load())/elapsed.Seconds())
	fmt.Printf("ingest to client  p50 %.2f ms  p90 %.2f ms  p99 %.2f ms  mean %.2f ms\n", s.P50Ms, s.P90Ms, s.P99Ms, s.MeanMs)
	if failed.Load() > 0 {
		os.Exit(1)
	}
}
