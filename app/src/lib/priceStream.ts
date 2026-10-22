import { Backoff } from './backoff';
import type { ConnectionStatus, Quote, ServerMessage } from './types';

// The subset of the WebSocket API this client uses, so tests can pass a fake.
export interface SocketLike {
  readyState: number;
  send(data: string): void;
  close(code?: number, reason?: string): void;
  onopen: (() => void) | null;
  onmessage: ((event: { data: unknown }) => void) | null;
  onclose: ((event: { code?: number }) => void) | null;
  onerror: ((event: unknown) => void) | null;
}

export type SocketFactory = (url: string) => SocketLike;

const OPEN = 1;

export interface PriceStreamOptions {
  url: string;
  // Incoming quotes are buffered (latest per symbol) and handed to the UI
  // at most once per window, so a busy market cannot re-render the list on
  // every tick.
  flushMs?: number;
  // Reconnect if an open socket has been silent this long while we follow
  // at least one symbol. Catches connections that died without a close
  // frame, which is common when a phone switches networks.
  staleMs?: number;
  backoff?: Backoff;
  createSocket?: SocketFactory;
}

type QuotesListener = (quotes: Quote[]) => void;
type StatusListener = (status: ConnectionStatus) => void;
type ErrorListener = (message: string) => void;

export class PriceStream {
  private readonly url: string;
  private readonly flushMs: number;
  private readonly staleMs: number;
  private readonly backoff: Backoff;
  private readonly createSocket: SocketFactory;

  private socket: SocketLike | null = null;
  private symbols = new Set<string>();
  private buffer = new Map<string, Quote>();
  private running = false;
  private statusValue: ConnectionStatus = 'idle';

  private flushTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private staleTimer: ReturnType<typeof setTimeout> | null = null;

  private quoteListeners = new Set<QuotesListener>();
  private statusListeners = new Set<StatusListener>();
  private errorListeners = new Set<ErrorListener>();

  constructor(opts: PriceStreamOptions) {
    this.url = opts.url;
    this.flushMs = opts.flushMs ?? 250;
    this.staleMs = opts.staleMs ?? 60_000;
    this.backoff = opts.backoff ?? new Backoff();
    this.createSocket =
      opts.createSocket ?? ((url) => new WebSocket(url) as unknown as SocketLike);
  }

  get status(): ConnectionStatus {
    return this.statusValue;
  }

  onQuotes(fn: QuotesListener): () => void {
    this.quoteListeners.add(fn);
    return () => this.quoteListeners.delete(fn);
  }

  onStatus(fn: StatusListener): () => void {
    this.statusListeners.add(fn);
    return () => this.statusListeners.delete(fn);
  }

  onError(fn: ErrorListener): () => void {
    this.errorListeners.add(fn);
    return () => this.errorListeners.delete(fn);
  }

  start(): void {
    if (this.running) return;
    this.running = true;
    this.backoff.reset();
    this.connect();
  }

  // Closes the socket and stops reconnecting, for example when the app
  // goes to the background.
  stop(): void {
    this.running = false;
    this.clearTimers();
    const s = this.socket;
    this.socket = null;
    if (s) {
      s.onclose = null;
      s.close(1000, 'client stopped');
    }
    this.flush();
    this.setStatus('idle');
  }

  // Replaces the watchlist. Changes are sent over the open socket; the full
  // list also goes in the URL of every new connection, so a reconnect
  // restores subscriptions without any extra round trip.
  setSymbols(next: Iterable<string>): void {
    const wanted = new Set(next);
    const added = [...wanted].filter((s) => !this.symbols.has(s));
    const removed = [...this.symbols].filter((s) => !wanted.has(s));
    this.symbols = wanted;
    for (const s of removed) this.buffer.delete(s);

    if (this.socket?.readyState === OPEN) {
      if (added.length) this.send({ type: 'subscribe', symbols: added });
      if (removed.length) this.send({ type: 'unsubscribe', symbols: removed });
    }
    this.armStale();
  }

  private connect(): void {
    this.setStatus(this.backoff.attempts === 0 ? 'connecting' : 'reconnecting');
    const query = this.symbols.size
      ? `?symbols=${encodeURIComponent([...this.symbols].join(','))}`
      : '';
    const s = this.createSocket(this.url + query);
    this.socket = s;

    s.onopen = () => {
      if (this.socket !== s) return;
      this.setStatus('open');
      this.armStale();
    };
    s.onmessage = (event) => {
      if (this.socket !== s) return;
      this.armStale();
      this.handle(event.data);
    };
    s.onerror = () => {
      // onclose always follows; reconnect is handled there.
    };
    s.onclose = () => {
      if (this.socket !== s) return;
      this.socket = null;
      this.scheduleReconnect();
    };
  }

  private handle(raw: unknown): void {
    let msg: ServerMessage;
    try {
      msg = JSON.parse(String(raw));
    } catch {
      return;
    }
    switch (msg.type) {
      case 'subscribed':
        // The server accepted us: treat the connection as healthy from
        // here, so the next failure starts from the shortest delay.
        this.backoff.reset();
        break;
      case 'prices':
        for (const q of msg.data ?? []) {
          if (!this.symbols.has(q.s)) continue;
          const prev = this.buffer.get(q.s);
          // Keep the newer quote if two for one symbol land in a window.
          if (!prev || Date.parse(prev.it) <= Date.parse(q.it)) this.buffer.set(q.s, q);
        }
        this.scheduleFlush();
        break;
      case 'error':
        for (const fn of this.errorListeners) fn(msg.message);
        break;
    }
  }

  private scheduleFlush(): void {
    if (this.flushTimer || this.buffer.size === 0) return;
    this.flushTimer = setTimeout(() => {
      this.flushTimer = null;
      this.flush();
    }, this.flushMs);
  }

  private flush(): void {
    if (this.buffer.size === 0) return;
    const batch = [...this.buffer.values()];
    this.buffer.clear();
    for (const fn of this.quoteListeners) fn(batch);
  }

  private scheduleReconnect(): void {
    if (!this.running) return;
    this.clearStale();
    const delay = this.backoff.next();
    this.setStatus('reconnecting');
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (this.running) this.connect();
    }, delay);
  }

  private armStale(): void {
    this.clearStale();
    if (!this.running || this.symbols.size === 0 || this.socket?.readyState !== OPEN) return;
    this.staleTimer = setTimeout(() => {
      // Silent too long: drop it and let onclose schedule a reconnect.
      this.socket?.close(4000, 'stale');
    }, this.staleMs);
  }

  private clearStale(): void {
    if (this.staleTimer) clearTimeout(this.staleTimer);
    this.staleTimer = null;
  }

  private clearTimers(): void {
    this.clearStale();
    if (this.flushTimer) clearTimeout(this.flushTimer);
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.flushTimer = null;
    this.reconnectTimer = null;
  }

  private send(msg: object): void {
    this.socket?.send(JSON.stringify(msg));
  }

  private setStatus(next: ConnectionStatus): void {
    if (next === this.statusValue) return;
    this.statusValue = next;
    for (const fn of this.statusListeners) fn(next);
  }
}
