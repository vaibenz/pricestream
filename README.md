# pricestream

Real time crypto prices, from exchange feeds to a mobile watchlist.

A Go ingestor reads Coinbase's public ticker feed and publishes to Redis. A Go gateway fans prices out to clients over WebSockets. A React Native app on iOS and Android shows a live watchlist with price alerts. The design goal is that slow clients and bursty markets never slow anyone else down: every hop keeps only the **latest price per symbol**, so backpressure turns into skipped stale ticks instead of growing queues.

```
Coinbase ws-feed ──> ingest ──> Redis ──> gateway ──WebSocket──> React Native app
  (ticker,            reconnect,  pub/sub    per client              250ms UI batching,
   heartbeat)         stall       + latest   coalescing,             reconnect + resubscribe,
                      detection   hash       frame cap               alerts
```

## Backend (Go)

### `cmd/ingest`

* Subscribes to the `ticker` and `heartbeat` channels on `wss://ws-feed.exchange.coinbase.com`.
* Heartbeats arrive every second, so a read deadline catches a socket that is open but silent and replaces it.
* Reconnects use capped exponential backoff with full jitter.
* Each quote is written to a Redis hash (latest per symbol) and published on a pub/sub channel in one pipelined round trip.
* `SOURCE=sim` swaps in a random walk feed for offline development and load tests.

### `cmd/gateway`

* **Per client coalescing** (`internal/hub`). Each client holds at most one pending quote per symbol. A burst of 100 BTC ticks before the client's writer runs becomes one frame with the newest price. Memory per client is bounded by its watchlist, never by how far behind it is, and publishing never blocks on a client.
* **Frame cap.** `MIN_INTERVAL` (default 100ms) limits how often a busy market wakes a phone's radio and UI. Updates arriving in between are coalesced.
* **Slow clients** whose socket stops draining are disconnected after a write timeout rather than buffered forever.
* **No gap on restart.** The Redis pump subscribes first and only then loads the snapshot hash. The hub ignores quotes older than what it holds, so the overlap is harmless.
* `GET /metrics` reports clients, coalesced updates, slow disconnects, and ingest to socket latency percentiles from a lock free histogram.

Protocol (JSON text frames):

```jsonc
// client -> server
{"type":"subscribe","symbols":["BTC-USD","ETH-USD"]}
{"type":"unsubscribe","symbols":["ETH-USD"]}
// server -> client
{"type":"subscribed","symbols":["BTC-USD"]}
{"type":"prices","data":[{"s":"BTC-USD","p":67012.34,"b":67012.33,"a":67012.35,"o":66000,"v":12345.6,"src":"coinbase","et":"...","it":"..."}]}
{"type":"error","message":"invalid symbol X"}
```

Symbols can also be passed up front: `/ws?symbols=BTC-USD,ETH-USD`.

## App (React Native, Expo)

* **Stream client** (`app/src/lib/priceStream.ts`):
  * reconnects with jittered backoff, resetting once the server acknowledges
  * puts the whole watchlist in the connection URL, so a reconnect restores subscriptions with no extra round trip
  * replaces sockets that go silent, which is common when a phone switches networks
* **Smooth rendering under high frequency updates.** Ticks are buffered and handed to React at most every 250ms. Rows are memoised, so a batch that only moves BTC re-renders only the BTC row. The green and red tick flash runs on the native animation driver, off the JS thread.
* **Battery and data.** The socket closes when the app goes to the background and reopens in the foreground.
* **Price alerts.** Tap a row to set a target. Direction is inferred from the current price. A one shot alert fires a haptic and a banner. Alerts and the watchlist persist in AsyncStorage and are validated on load.
* Light and dark themes, accessibility labels on interactive elements.

The stream client, alert logic and formatting are plain TypeScript with no React Native imports. They are tested under Node with a fake socket and mocked timers (`npm test`), covering batching, diffed subscriptions, backoff growth and reset, stall replacement, and stop.

## Running it

```bash
docker compose up --build -d            # Redis + ingest (live Coinbase) + gateway on :8080
SOURCE=sim docker compose up --build -d # or fully offline with simulated prices

cd app && npm install
EXPO_PUBLIC_GATEWAY_URL=ws://<your-LAN-IP>:8080/ws npx expo start
```

Scan the QR code with Expo Go on a phone on the same network. The Android emulator reaches the host at `10.0.2.2`.

Tests: `make test` runs the Go suite under the race detector (Redis integration tests need `REDIS_URL`; they are skipped without it) plus the app's typecheck and unit tests.

## Benchmark

`cmd/loadtest` opens N WebSocket clients and measures latency from the ingestor receiving a tick to a client decoding it. Setup: simulated feed at 150 ticks/s across 3 symbols, every client following all 3, and everything (Redis, ingestor, gateway and the load generator) on one 2 vCPU cloud VM.

| Clients | Frame cap | Quotes delivered | Ingest to client p50 / p99 | Slow disconnects |
|---|---|---|---|---|
| 100 | none | 13.6k/s | ≤2 ms / ≤10 ms | 0 |
| 1,000 | none | 83.8k/s | ≤50 ms / ≤200 ms | 0 |
| 1,000 | 100 ms | 29.4k/s | ≤20 ms / ≤100 ms | 0 |

Latencies are histogram bucket upper bounds. At 1,000 uncapped clients the box is CPU bound, since the load generator shares the two cores. Coalescing absorbs the shortfall (about 1.06M stale ticks skipped instead of queued), so every client still holds the latest price and nobody is disconnected.

## Limits and next steps

* **One gateway** holds all its clients in memory. Horizontal scaling works as is (each gateway subscribes to Redis independently) behind a load balancer with WebSocket support.
* **Prices are float64.** That is fine for display. Anything that settles money should carry exact decimals end to end.
* **Push notifications** for alerts while the app is closed would need the alert rules on the server and a push service such as APNs or FCM. Today, alerts only fire while the app is open.
