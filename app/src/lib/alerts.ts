import type { Quote } from './types';

export type AlertDirection = 'above' | 'below';

export interface PriceAlert {
  id: string;
  symbol: string;
  direction: AlertDirection;
  target: number;
  createdAt: number;
  // Alerts fire once. A fired alert stays listed until the user removes it.
  firedAt?: number;
  firedPrice?: number;
}

export function newAlert(
  symbol: string,
  direction: AlertDirection,
  target: number,
  now = Date.now(),
): PriceAlert {
  return {
    id: `${symbol}:${direction}:${target}:${now}`,
    symbol,
    direction,
    target,
    createdAt: now,
  };
}

// Checks a batch of quotes against the alerts. Pure, so the same logic runs
// in tests and in the app. Returns the updated list and the alerts that fired
// in this batch.
export function checkAlerts(
  alerts: PriceAlert[],
  quotes: Quote[],
  now = Date.now(),
): { alerts: PriceAlert[]; fired: PriceAlert[] } {
  if (alerts.length === 0 || quotes.length === 0) return { alerts, fired: [] };
  const latest = new Map(quotes.map((q) => [q.s, q.p]));
  const fired: PriceAlert[] = [];

  const next = alerts.map((a) => {
    if (a.firedAt !== undefined) return a;
    const price = latest.get(a.symbol);
    if (price === undefined) return a;
    const hit = a.direction === 'above' ? price >= a.target : price <= a.target;
    if (!hit) return a;
    const updated = { ...a, firedAt: now, firedPrice: price };
    fired.push(updated);
    return updated;
  });
  return { alerts: fired.length ? next : alerts, fired };
}

export function describeAlert(a: PriceAlert, format: (n: number) => string): string {
  return `${a.symbol} ${a.direction} ${format(a.target)}`;
}
