const big = new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const mid = new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 4 });
const small = new Intl.NumberFormat('en-US', { maximumSignificantDigits: 4 });

// Adapts decimals to magnitude: 67,012.34 · 1.2345 · 0.00001234
export function formatPrice(p: number): string {
  if (!Number.isFinite(p)) return '...';
  const abs = Math.abs(p);
  if (abs >= 1000) return big.format(p);
  if (abs >= 1) return mid.format(p);
  if (abs === 0) return '0.00';
  return small.format(p);
}

export function changeSince(price: number, open?: number): number | undefined {
  if (!open) return undefined;
  return (price - open) / open;
}

export function formatChange(fraction: number | undefined): string {
  if (fraction === undefined || !Number.isFinite(fraction)) return '';
  const pct = fraction * 100;
  const sign = pct > 0 ? '+' : '';
  return `${sign}${pct.toFixed(2)}%`;
}

const SYMBOL = /^[A-Z0-9]{2,10}-[A-Z0-9]{2,10}$/;

// Accepts "btc", "BTC-USD" or "eth-usdt"; a bare base defaults to USD.
// Returns undefined for anything the gateway would reject.
export function normalizeSymbol(input: string): string | undefined {
  let s = input.trim().toUpperCase().replace(/\s+/g, '').replace('/', '-');
  if (!s.includes('-')) s = `${s}-USD`;
  return SYMBOL.test(s) ? s : undefined;
}
