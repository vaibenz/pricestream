import assert from 'node:assert/strict';
import { test } from 'node:test';

import { checkAlerts, newAlert } from './alerts';
import type { Quote } from './types';

const q = (s: string, p: number): Quote => ({ s, p, src: 't', et: '', it: '' });

test('fires an above alert once when the price reaches the target', () => {
  let alerts = [newAlert('BTC-USD', 'above', 70_000, 1)];

  let r = checkAlerts(alerts, [q('BTC-USD', 69_999.99)], 10);
  assert.equal(r.fired.length, 0);
  assert.equal(r.alerts, alerts, 'unchanged list is returned as is, so React skips a render');

  r = checkAlerts(alerts, [q('BTC-USD', 70_000)], 20);
  assert.equal(r.fired.length, 1);
  assert.equal(r.fired[0].firedAt, 20);
  assert.equal(r.fired[0].firedPrice, 70_000);
  alerts = r.alerts;

  r = checkAlerts(alerts, [q('BTC-USD', 71_000)], 30);
  assert.equal(r.fired.length, 0, 'fires only once');
});

test('below alerts and unrelated symbols', () => {
  const alerts = [newAlert('ETH-USD', 'below', 2_500), newAlert('SOL-USD', 'above', 200)];
  const r = checkAlerts(alerts, [q('ETH-USD', 2_499), q('BTC-USD', 1)]);
  assert.deepEqual(
    r.fired.map((a) => a.symbol),
    ['ETH-USD'],
  );
  assert.equal(r.alerts[1], alerts[1]);
});
