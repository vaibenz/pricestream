import assert from 'node:assert/strict';
import { test } from 'node:test';

import { Backoff } from './backoff';
import { changeSince, formatChange, formatPrice, normalizeSymbol } from './format';
import { DEFAULT_WATCHLIST, parseSaved } from './persist';

test('formatPrice adapts decimals to magnitude', () => {
  assert.equal(formatPrice(67012.345), '67,012.35');
  assert.equal(formatPrice(1.23456), '1.2346');
  assert.equal(formatPrice(170), '170.00');
  assert.equal(formatPrice(0.0000123456), '0.00001235');
  assert.equal(formatPrice(Number.NaN), '...');
});

test('formatChange signs and rounds', () => {
  assert.equal(formatChange(changeSince(101, 100)), '+1.00%');
  assert.equal(formatChange(changeSince(99.5, 100)), '-0.50%');
  assert.equal(formatChange(changeSince(100, undefined)), '');
});

test('normalizeSymbol', () => {
  assert.equal(normalizeSymbol(' btc '), 'BTC-USD');
  assert.equal(normalizeSymbol('eth/usdt'), 'ETH-USDT');
  assert.equal(normalizeSymbol('SOL-USD'), 'SOL-USD');
  assert.equal(normalizeSymbol('x'), undefined);
  assert.equal(normalizeSymbol('btc_usd!'), undefined);
});

test('parseSaved degrades to defaults on bad data', () => {
  assert.deepEqual(parseSaved(null).watchlist, DEFAULT_WATCHLIST);
  assert.deepEqual(parseSaved('{not json').watchlist, DEFAULT_WATCHLIST);
  const s = parseSaved(
    JSON.stringify({
      watchlist: ['btc', 'BTC-USD', 'bad!', 42],
      alerts: [
        { id: 'a', symbol: 'BTC-USD', direction: 'above', target: 1, createdAt: 1 },
        { id: 'b', symbol: 'BTC-USD', direction: 'sideways', target: 1, createdAt: 1 },
      ],
    }),
  );
  assert.deepEqual(s.watchlist, ['BTC-USD']);
  assert.deepEqual(
    s.alerts.map((a) => a.id),
    ['a'],
  );
});

test('backoff grows, caps and resets', () => {
  const b = new Backoff(100, 1000, () => 1);
  assert.deepEqual(
    [b.next(), b.next(), b.next(), b.next(), b.next(), b.next()],
    [150, 250, 450, 850, 1050, 1050],
  );
  b.reset();
  assert.equal(b.next(), 150);
});
