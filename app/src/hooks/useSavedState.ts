import AsyncStorage from '@react-native-async-storage/async-storage';
import { useEffect, useRef, useState } from 'react';

import { parseSaved, type SavedState } from '../lib/persist';

const KEY = 'pricestream/state/v1';

// Loads the watchlist and alerts once, then saves changes after a short
// debounce so a burst of edits is one write.
export function useSavedState() {
  const [state, setState] = useState<SavedState | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    let cancelled = false;
    AsyncStorage.getItem(KEY)
      .catch(() => null)
      .then((raw) => {
        if (!cancelled) setState(parseSaved(raw));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!state) return;
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      AsyncStorage.setItem(KEY, JSON.stringify(state)).catch(() => {});
    }, 300);
  }, [state]);

  return [state, setState] as const;
}
