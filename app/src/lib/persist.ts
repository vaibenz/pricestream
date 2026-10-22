import type { PriceAlert } from './alerts';
import { normalizeSymbol } from './format';

export const DEFAULT_WATCHLIST = ['BTC-USD', 'ETH-USD', 'SOL-USD'];

export interface SavedState {
  watchlist: string[];
  alerts: PriceAlert[];
}

// Validates whatever came back from storage. Storage survives app updates,
// so old or corrupted data must degrade to defaults rather than crash.
export function parseSaved(raw: string | null): SavedState {
  const fallback: SavedState = { watchlist: [...DEFAULT_WATCHLIST], alerts: [] };
  if (!raw) return fallback;
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return fallback;
  }
  if (typeof data !== 'object' || data === null) return fallback;
  const obj = data as Record<string, unknown>;

  const watchlist = Array.isArray(obj.watchlist)
    ? [...new Set(obj.watchlist.map((s) => (typeof s === 'string' ? normalizeSymbol(s) : undefined)))]
        .filter((s): s is string => !!s)
        .slice(0, 50)
    : fallback.watchlist;

  const alerts = Array.isArray(obj.alerts) ? obj.alerts.filter(isAlert) : [];
  return { watchlist, alerts };
}

function isAlert(a: unknown): a is PriceAlert {
  if (typeof a !== 'object' || a === null) return false;
  const x = a as Record<string, unknown>;
  return (
    typeof x.id === 'string' &&
    typeof x.symbol === 'string' &&
    (x.direction === 'above' || x.direction === 'below') &&
    typeof x.target === 'number' &&
    Number.isFinite(x.target) &&
    typeof x.createdAt === 'number'
  );
}
