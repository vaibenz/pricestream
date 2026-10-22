// Wire format from the gateway. Keys are short because every frame carries
// them; see internal/quote/quote.go on the server.
export interface Quote {
  s: string; // symbol, e.g. "BTC-USD"
  p: number; // last price
  b?: number; // best bid
  a?: number; // best ask
  o?: number; // 24h open
  v?: number; // 24h volume
  src: string;
  et: string; // exchange time, RFC 3339
  it: string; // ingest time, RFC 3339
}

export type ServerMessage =
  | { type: 'prices'; data: Quote[] }
  | { type: 'subscribed'; symbols?: string[] }
  | { type: 'error'; message: string };

export type ConnectionStatus = 'idle' | 'connecting' | 'open' | 'reconnecting';
