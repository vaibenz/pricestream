import { useEffect, useRef, useState } from 'react';
import { AppState } from 'react-native';

import { PriceStream } from '../lib/priceStream';
import type { ConnectionStatus, Quote } from '../lib/types';

// Owns one PriceStream for the app's lifetime. The socket is closed while
// the app is in the background, which saves battery and data, and reopened
// when it returns to the foreground.
export function usePriceStream(
  url: string,
  symbols: string[],
  onBatch?: (quotes: Quote[]) => void,
) {
  const streamRef = useRef<PriceStream | null>(null);
  if (!streamRef.current) streamRef.current = new PriceStream({ url });
  const stream = streamRef.current;

  const [prices, setPrices] = useState<Record<string, Quote>>({});
  const [status, setStatus] = useState<ConnectionStatus>(stream.status);
  const [error, setError] = useState<string | null>(null);

  // Keep the latest callback without re-subscribing on every render.
  const onBatchRef = useRef(onBatch);
  onBatchRef.current = onBatch;

  useEffect(() => {
    const offQuotes = stream.onQuotes((batch) => {
      setPrices((prev) => {
        const next = { ...prev };
        for (const q of batch) next[q.s] = q;
        return next;
      });
      onBatchRef.current?.(batch);
    });
    const offStatus = stream.onStatus(setStatus);
    const offError = stream.onError(setError);

    if (AppState.currentState !== 'background') stream.start();
    const sub = AppState.addEventListener('change', (state) => {
      if (state === 'active') stream.start();
      else if (state === 'background') stream.stop();
    });

    return () => {
      sub.remove();
      offQuotes();
      offStatus();
      offError();
      stream.stop();
    };
  }, [stream]);

  const key = symbols.join(',');
  useEffect(() => {
    stream.setSymbols(symbols);
    // Drop prices for removed symbols so stale rows do not linger.
    setPrices((prev) => {
      const next: Record<string, Quote> = {};
      for (const s of symbols) if (prev[s]) next[s] = prev[s];
      return next;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stream, key]);

  return { prices, status, error, clearError: () => setError(null) };
}
