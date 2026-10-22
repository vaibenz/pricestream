import assert from 'node:assert/strict';
import { beforeEach, describe, mock, test } from 'node:test';

import { Backoff } from './backoff';
import { PriceStream, type SocketLike } from './priceStream';
import type { Quote } from './types';

class FakeSocket implements SocketLike {
  readyState = 0;
  sent: unknown[] = [];
  closed = false;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: ((event: { code?: number }) => void) | null = null;
  onerror: ((event: unknown) => void) | null = null;

  constructor(public url: string) {}

  send(data: string) {
    this.sent.push(JSON.parse(data));
  }
  close() {
    if (this.closed) return;
    this.closed = true;
    this.readyState = 3;
    this.onclose?.({ code: 1006 });
  }
  // Test helpers acting as the server.
  open() {
    this.readyState = 1;
    this.onopen?.();
  }
  push(msg: object) {
    this.onmessage?.({ data: JSON.stringify(msg) });
  }
}

const quote = (s: string, p: number, it = new Date().toISOString()): Quote => ({
  s,
  p,
  src: 'test',
  et: it,
  it,
});

describe('PriceStream', () => {
  let sockets: FakeSocket[];
  let stream: PriceStream;
  let batches: Quote[][];
  let statuses: string[];

  beforeEach(() => {
    mock.timers.reset();
    mock.timers.enable({ apis: ['setTimeout'] });
    sockets = [];
    batches = [];
    statuses = [];
    stream = new PriceStream({
      url: 'ws://gw/ws',
      flushMs: 250,
      staleMs: 10_000,
      backoff: new Backoff(1000, 8000, () => 0.5),
      createSocket: (url) => {
        const s = new FakeSocket(url);
        sockets.push(s);
        return s;
      },
    });
    stream.onQuotes((q) => batches.push(q));
    stream.onStatus((s) => statuses.push(s));
  });

  test('connects with the watchlist in the URL', () => {
    stream.setSymbols(['BTC-USD', 'ETH-USD']);
    stream.start();
    assert.equal(sockets.length, 1);
    assert.equal(sockets[0].url, 'ws://gw/ws?symbols=BTC-USD%2CETH-USD');
    assert.deepEqual(statuses, ['connecting']);
    sockets[0].open();
    assert.equal(stream.status, 'open');
  });

  test('batches a burst into one UI update with the latest price per symbol', () => {
    stream.setSymbols(['BTC-USD', 'ETH-USD']);
    stream.start();
    sockets[0].open();

    for (let i = 0; i < 50; i++) {
      sockets[0].push({ type: 'prices', data: [quote('BTC-USD', 60_000 + i)] });
    }
    sockets[0].push({ type: 'prices', data: [quote('ETH-USD', 3_000)] });
    assert.equal(batches.length, 0, 'nothing delivered before the window ends');

    mock.timers.tick(250);
    assert.equal(batches.length, 1);
    const bySymbol = Object.fromEntries(batches[0].map((q) => [q.s, q.p]));
    assert.deepEqual(bySymbol, { 'BTC-USD': 60_049, 'ETH-USD': 3_000 });
  });

  test('ignores quotes for symbols no longer followed', () => {
    stream.setSymbols(['BTC-USD']);
    stream.start();
    sockets[0].open();
    sockets[0].push({ type: 'prices', data: [quote('DOGE-USD', 0.1)] });
    mock.timers.tick(250);
    assert.equal(batches.length, 0);
  });

  test('sends subscribe and unsubscribe diffs over an open socket', () => {
    stream.setSymbols(['BTC-USD', 'ETH-USD']);
    stream.start();
    sockets[0].open();
    stream.setSymbols(['ETH-USD', 'SOL-USD']);
    assert.deepEqual(sockets[0].sent, [
      { type: 'subscribe', symbols: ['SOL-USD'] },
      { type: 'unsubscribe', symbols: ['BTC-USD'] },
    ]);
  });

  test('reconnects with backoff and restores subscriptions', () => {
    stream.setSymbols(['BTC-USD']);
    stream.start();
    sockets[0].open();
    stream.setSymbols(['BTC-USD', 'SOL-USD']);

    sockets[0].close(); // network drop
    assert.equal(stream.status, 'reconnecting');
    // Backoff(1000, ...) with random 0.5: first delay is 500 + 500 = 1000ms.
    mock.timers.tick(999);
    assert.equal(sockets.length, 1);
    mock.timers.tick(1);
    assert.equal(sockets.length, 2);
    assert.equal(sockets[1].url, 'ws://gw/ws?symbols=BTC-USD%2CSOL-USD');

    // Fails again before the server acknowledges: the delay grows.
    sockets[1].close();
    mock.timers.tick(1499);
    assert.equal(sockets.length, 2);
    mock.timers.tick(1);
    assert.equal(sockets.length, 3);

    // Once acknowledged, the next failure starts from the shortest delay.
    sockets[2].open();
    sockets[2].push({ type: 'subscribed', symbols: ['BTC-USD', 'SOL-USD'] });
    sockets[2].close();
    mock.timers.tick(1000);
    assert.equal(sockets.length, 4);
  });

  test('replaces a socket that goes silent', () => {
    stream.setSymbols(['BTC-USD']);
    stream.start();
    sockets[0].open();
    mock.timers.tick(9_000);
    sockets[0].push({ type: 'prices', data: [quote('BTC-USD', 1)] }); // resets the timer
    mock.timers.tick(9_000);
    assert.equal(sockets[0].closed, false);
    mock.timers.tick(1_000);
    assert.equal(sockets[0].closed, true, 'closed after 10s of silence');
    mock.timers.tick(1_000);
    assert.equal(sockets.length, 2, 'and reconnected');
  });

  test('stop closes the socket, flushes, and does not reconnect', () => {
    stream.setSymbols(['BTC-USD']);
    stream.start();
    sockets[0].open();
    sockets[0].push({ type: 'prices', data: [quote('BTC-USD', 1)] });
    stream.stop();
    assert.equal(batches.length, 1, 'pending quotes flushed on stop');
    assert.equal(sockets[0].closed, true);
    mock.timers.tick(60_000);
    assert.equal(sockets.length, 1);
    assert.equal(stream.status, 'idle');
  });

  test('reports server errors', () => {
    const errors: string[] = [];
    stream.onError((m) => errors.push(m));
    stream.start();
    sockets[0].open();
    sockets[0].push({ type: 'error', message: 'invalid symbol X' });
    sockets[0].onmessage?.({ data: 'not json' });
    assert.deepEqual(errors, ['invalid symbol X']);
  });
});
