import { useEffect, useState } from 'react';
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import type { AlertDirection, PriceAlert } from '../lib/alerts';
import { formatPrice } from '../lib/format';
import type { Quote } from '../lib/types';
import type { Theme } from '../theme';

interface Props {
  symbol: string | null;
  quote?: Quote;
  alerts: PriceAlert[];
  theme: Theme;
  onCreate: (symbol: string, direction: AlertDirection, target: number) => void;
  onDelete: (id: string) => void;
  onClose: () => void;
}

export function AlertSheet({ symbol, quote, alerts, theme, onCreate, onDelete, onClose }: Props) {
  const [target, setTarget] = useState('');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setTarget('');
    setError(null);
  }, [symbol]);

  if (!symbol) return null;
  const mine = alerts.filter((a) => a.symbol === symbol);

  const create = () => {
    const value = Number(target.replace(/,/g, ''));
    if (!Number.isFinite(value) || value <= 0) {
      setError('Enter a price above zero');
      return;
    }
    if (!quote) {
      setError('Waiting for a live price first');
      return;
    }
    if (value === quote.p) {
      setError('That is the current price');
      return;
    }
    // Direction follows from where the target sits relative to now.
    onCreate(symbol, value > quote.p ? 'above' : 'below', value);
    setTarget('');
    setError(null);
  };

  return (
    <Modal visible animationType="slide" transparent onRequestClose={onClose}>
      <Pressable style={styles.backdrop} onPress={onClose} accessibilityLabel="Close" />
      <KeyboardAvoidingView behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <View style={[styles.sheet, { backgroundColor: theme.card }]}>
          <Text style={[styles.title, { color: theme.text }]}>{symbol} alerts</Text>
          <Text style={[styles.sub, { color: theme.muted }]}>
            Now {quote ? formatPrice(quote.p) : '...'}. You get one alert when the price crosses
            your target.
          </Text>

          <View style={[styles.inputRow, { borderColor: theme.border }]}>
            <TextInput
              value={target}
              onChangeText={setTarget}
              onSubmitEditing={create}
              placeholder="Target price"
              placeholderTextColor={theme.muted}
              keyboardType="decimal-pad"
              style={[styles.input, { color: theme.text }]}
              accessibilityLabel="Target price"
            />
            <Pressable onPress={create} accessibilityRole="button" hitSlop={8}>
              <Text style={[styles.action, { color: theme.accent }]}>Set alert</Text>
            </Pressable>
          </View>
          {error ? <Text style={[styles.error, { color: theme.down }]}>{error}</Text> : null}

          <ScrollView style={styles.list}>
            {mine.length === 0 ? (
              <Text style={[styles.empty, { color: theme.muted }]}>No alerts yet.</Text>
            ) : (
              mine.map((a) => (
                <View key={a.id} style={[styles.alert, { borderColor: theme.border }]}>
                  <Text style={[styles.alertText, { color: theme.text }]}>
                    {a.direction === 'above' ? 'Rises to' : 'Falls to'} {formatPrice(a.target)}
                    {a.firedAt ? `  ·  hit at ${formatPrice(a.firedPrice ?? a.target)}` : ''}
                  </Text>
                  <Pressable
                    onPress={() => onDelete(a.id)}
                    accessibilityRole="button"
                    accessibilityLabel="Remove alert"
                    hitSlop={8}
                  >
                    <Text style={{ color: theme.down }}>Remove</Text>
                  </Pressable>
                </View>
              ))
            )}
          </ScrollView>
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: { flex: 1, backgroundColor: 'rgba(0,0,0,0.35)' },
  sheet: {
    borderTopLeftRadius: 20,
    borderTopRightRadius: 20,
    padding: 20,
    paddingBottom: 36,
    maxHeight: 480,
  },
  title: { fontSize: 20, fontWeight: '700' },
  sub: { fontSize: 14, marginTop: 4, marginBottom: 16, lineHeight: 20 },
  inputRow: {
    flexDirection: 'row',
    alignItems: 'center',
    borderWidth: 1,
    borderRadius: 12,
    paddingHorizontal: 14,
  },
  input: { flex: 1, paddingVertical: 12, fontSize: 16 },
  action: { fontWeight: '600', fontSize: 15, paddingLeft: 12 },
  error: { marginTop: 8, fontSize: 13 },
  list: { marginTop: 16 },
  empty: { fontSize: 14 },
  alert: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  alertText: { fontSize: 15, flex: 1 },
});
