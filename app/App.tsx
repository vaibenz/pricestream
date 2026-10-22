import * as Haptics from 'expo-haptics';
import { StatusBar } from 'expo-status-bar';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, Alert, FlatList, StyleSheet, Text, View } from 'react-native';
import { SafeAreaProvider, SafeAreaView } from 'react-native-safe-area-context';

import { AddSymbol } from './src/components/AddSymbol';
import { AlertSheet } from './src/components/AlertSheet';
import { PriceRow } from './src/components/PriceRow';
import { StatusPill } from './src/components/StatusPill';
import { Toast } from './src/components/Toast';
import { GATEWAY_URL } from './src/config';
import { usePriceStream } from './src/hooks/usePriceStream';
import { useSavedState } from './src/hooks/useSavedState';
import { checkAlerts, newAlert, type AlertDirection } from './src/lib/alerts';
import { formatPrice } from './src/lib/format';
import type { Quote } from './src/lib/types';
import { useTheme } from './src/theme';

export default function App() {
  return (
    <SafeAreaProvider>
      <Watchlist />
    </SafeAreaProvider>
  );
}

function Watchlist() {
  const theme = useTheme();
  const [saved, setSaved] = useSavedState();
  const [sheetSymbol, setSheetSymbol] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const watchlist = saved?.watchlist ?? [];

  // Alerts are checked against every batch from the stream. A ref keeps
  // the check off React's render path.
  const savedRef = useRef(saved);
  savedRef.current = saved;
  const onBatch = useCallback(
    (quotes: Quote[]) => {
      const current = savedRef.current;
      if (!current) return;
      const { alerts, fired } = checkAlerts(current.alerts, quotes);
      if (fired.length === 0) return;
      setSaved({ ...current, alerts });
      const first = fired[0];
      setToast(
        `${first.symbol} ${first.direction === 'above' ? 'rose to' : 'fell to'} ${formatPrice(first.firedPrice ?? first.target)}` +
          (fired.length > 1 ? ` (+${fired.length - 1} more)` : ''),
      );
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success).catch(() => {});
    },
    [setSaved],
  );

  const { prices, status, error, clearError } = usePriceStream(GATEWAY_URL, watchlist, onBatch);

  useEffect(() => {
    if (error) {
      setToast(error);
      clearError();
    }
  }, [error, clearError]);

  const alertCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const a of saved?.alerts ?? []) {
      if (!a.firedAt) counts[a.symbol] = (counts[a.symbol] ?? 0) + 1;
    }
    return counts;
  }, [saved?.alerts]);

  const add = useCallback(
    (symbol: string) => setSaved((s) => s && { ...s, watchlist: [...s.watchlist, symbol] }),
    [setSaved],
  );

  const confirmRemove = useCallback(
    (symbol: string) => {
      Alert.alert(`Remove ${symbol}?`, 'Its alerts will be removed too.', [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Remove',
          style: 'destructive',
          onPress: () =>
            setSaved(
              (s) =>
                s && {
                  watchlist: s.watchlist.filter((x) => x !== symbol),
                  alerts: s.alerts.filter((a) => a.symbol !== symbol),
                },
            ),
        },
      ]);
    },
    [setSaved],
  );

  const createAlert = (symbol: string, direction: AlertDirection, target: number) =>
    setSaved((s) => s && { ...s, alerts: [...s.alerts, newAlert(symbol, direction, target)] });
  const deleteAlert = (id: string) =>
    setSaved((s) => s && { ...s, alerts: s.alerts.filter((a) => a.id !== id) });
  const hideToast = useCallback(() => setToast(null), []);

  return (
    <SafeAreaView style={[styles.screen, { backgroundColor: theme.bg }]} edges={['top', 'left', 'right']}>
      <StatusBar style="auto" />
      <View style={styles.header}>
        <Text style={[styles.title, { color: theme.text }]}>Watchlist</Text>
        <StatusPill status={status} theme={theme} />
      </View>

      {!saved ? (
        <ActivityIndicator style={styles.loading} color={theme.muted} />
      ) : (
        <FlatList
          data={watchlist}
          keyExtractor={(s) => s}
          keyboardShouldPersistTaps="handled"
          ListHeaderComponent={<AddSymbol theme={theme} existing={watchlist} onAdd={add} />}
          ListEmptyComponent={
            <Text style={[styles.empty, { color: theme.muted }]}>
              Add a market above to start streaming prices.
            </Text>
          }
          renderItem={({ item }) => (
            <PriceRow
              symbol={item}
              quote={prices[item]}
              alertCount={alertCounts[item] ?? 0}
              theme={theme}
              onPress={setSheetSymbol}
              onLongPress={confirmRemove}
            />
          )}
          contentContainerStyle={styles.list}
        />
      )}

      <AlertSheet
        symbol={sheetSymbol}
        quote={sheetSymbol ? prices[sheetSymbol] : undefined}
        alerts={saved?.alerts ?? []}
        theme={theme}
        onCreate={createAlert}
        onDelete={deleteAlert}
        onClose={() => setSheetSymbol(null)}
      />
      <Toast message={toast} theme={theme} onHide={hideToast} />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 20,
    paddingTop: 8,
    paddingBottom: 16,
  },
  title: { fontSize: 30, fontWeight: '800' },
  loading: { marginTop: 40 },
  list: { paddingBottom: 40 },
  empty: { textAlign: 'center', marginTop: 24, fontSize: 15 },
});
