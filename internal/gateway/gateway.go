// Package gateway serves live quotes to clients over WebSockets.
//
// Protocol (JSON text frames):
//
//	client -> {"type":"subscribe","symbols":["BTC-USD"]}
//	client -> {"type":"unsubscribe","symbols":["BTC-USD"]}
//	server -> {"type":"subscribed","symbols":[...]}       current watchlist
//	server -> {"type":"prices","data":[quote, ...]}       latest per symbol
//	server -> {"type":"error","message":"..."}
//
// Symbols can also be given up front with /ws?symbols=BTC-USD,ETH-USD.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/vaibenz/pricestream/internal/hub"
	"github.com/vaibenz/pricestream/internal/metrics"
	"github.com/vaibenz/pricestream/internal/quote"
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,10}-[A-Z0-9]{2,10}$`)

type Config struct {
	// MinInterval is the shortest gap between two frames to one client.
	// Updates arriving inside the gap are coalesced into the next frame,
	// which caps how hard a busy market can drive a phone's radio and UI.
	MinInterval time.Duration
	// WriteTimeout disconnects a client whose socket stops draining.
	WriteTimeout time.Duration
	PingInterval time.Duration
	MaxSymbols   int
	// OriginPatterns lists browser origins allowed to connect. Native
	// apps send no Origin header and are always allowed.
	OriginPatterns []string
}

func (c *Config) defaults() {
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 5 * time.Second
	}
	if c.PingInterval == 0 {
		c.PingInterval = 20 * time.Second
	}
	if c.MaxSymbols == 0 {
		c.MaxSymbols = 50
	}
}

type Server struct {
	hub *hub.Hub
	cfg Config
	log *slog.Logger

	// Latency from our ingestor receiving a tick to it being written to a
	// client's socket.
	latency        metrics.Histogram
	framesSent     atomic.Int64
	slowDisconnect atomic.Int64
}

func New(h *hub.Hub, cfg Config, log *slog.Logger) *Server {
	cfg.defaults()
	return &Server{hub: h, cfg: cfg, log: log}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", s.serveWS)
	mux.HandleFunc("GET /v1/prices", s.servePrices)
	mux.HandleFunc("GET /metrics", s.serveMetrics)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	})
	return mux
}

func (s *Server) servePrices(w http.ResponseWriter, r *http.Request) {
	syms, _ := parseSymbols(r.URL.Query().Get("symbols"), s.cfg.MaxSymbols)
	writeJSON(w, map[string]any{"data": s.hub.Latest(syms...)})
}

func (s *Server) serveMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"hub":                    s.hub.Stats(),
		"frames_sent":            s.framesSent.Load(),
		"slow_client_disconnect": s.slowDisconnect.Load(),
		"ingest_to_socket":       s.latency.Summary(),
	})
}

type clientMsg struct {
	Type    string   `json:"type"`
	Symbols []string `json:"symbols"`
}

type serverMsg struct {
	Type    string        `json:"type"`
	Data    []quote.Quote `json:"data,omitempty"`
	Symbols []string      `json:"symbols,omitempty"`
	Message string        `json:"message,omitempty"`
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	initial, err := parseSymbols(r.URL.Query().Get("symbols"), s.cfg.MaxSymbols)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:  s.cfg.OriginPatterns,
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4096)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	c := s.hub.Register()
	defer s.hub.Unregister(c)
	if len(initial) > 0 {
		s.hub.Subscribe(c, initial...)
	}
	s.send(ctx, conn, serverMsg{Type: "subscribed", Symbols: c.Symbols()})

	go s.readLoop(ctx, cancel, conn, c)
	go s.pingLoop(ctx, cancel, conn)

	err = s.writeLoop(ctx, conn, c)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		s.slowDisconnect.Add(1)
		conn.Close(websocket.StatusTryAgainLater, "client too slow")
	case ctx.Err() != nil:
		conn.Close(websocket.StatusNormalClosure, "")
	}
}

func (s *Server) writeLoop(ctx context.Context, conn *websocket.Conn, c *hub.Client) error {
	for {
		batch, err := c.Next(ctx)
		if err != nil {
			return err
		}
		if err := s.send(ctx, conn, serverMsg{Type: "prices", Data: batch}); err != nil {
			return err
		}
		now := time.Now()
		for _, q := range batch {
			s.latency.Observe(now.Sub(q.IngestTime))
		}
		s.framesSent.Add(1)

		if s.cfg.MinInterval > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s.cfg.MinInterval):
			}
		}
	}
}

func (s *Server) readLoop(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, c *hub.Client) {
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var m clientMsg
		if err := json.Unmarshal(data, &m); err != nil {
			s.send(ctx, conn, serverMsg{Type: "error", Message: "invalid JSON"})
			continue
		}
		syms, err := validate(m.Symbols, s.cfg.MaxSymbols)
		if err != nil {
			s.send(ctx, conn, serverMsg{Type: "error", Message: err.Error()})
			continue
		}
		switch m.Type {
		case "subscribe":
			if len(c.Symbols())+len(syms) > s.cfg.MaxSymbols {
				s.send(ctx, conn, serverMsg{Type: "error", Message: "too many symbols"})
				continue
			}
			s.hub.Subscribe(c, syms...)
		case "unsubscribe":
			s.hub.Unsubscribe(c, syms...)
		default:
			s.send(ctx, conn, serverMsg{Type: "error", Message: "unknown message type"})
			continue
		}
		s.send(ctx, conn, serverMsg{Type: "subscribed", Symbols: c.Symbols()})
	}
}

// pingLoop keeps idle connections alive through proxies and load balancers
// and notices a peer that vanished without closing.
func (s *Server) pingLoop(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn) {
	t := time.NewTicker(s.cfg.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, pcancel := context.WithTimeout(ctx, s.cfg.WriteTimeout)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

func (s *Server) send(ctx context.Context, conn *websocket.Conn, m serverMsg) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, s.cfg.WriteTimeout)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, data)
}

func parseSymbols(raw string, max int) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	return validate(strings.Split(raw, ","), max)
}

func validate(syms []string, max int) ([]string, error) {
	if len(syms) > max {
		return nil, errors.New("too many symbols")
	}
	out := make([]string, 0, len(syms))
	for _, s := range syms {
		s = strings.ToUpper(strings.TrimSpace(s))
		if !symbolPattern.MatchString(s) {
			return nil, errors.New("invalid symbol " + s)
		}
		out = append(out, s)
	}
	return out, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
