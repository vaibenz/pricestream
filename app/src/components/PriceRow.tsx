import { memo, useEffect, useRef } from 'react';
import { Animated, Pressable, StyleSheet, Text, View } from 'react-native';

import { changeSince, formatChange, formatPrice } from '../lib/format';
import type { Quote } from '../lib/types';
import type { Theme } from '../theme';

interface Props {
  symbol: string;
  quote?: Quote;
  alertCount: number;
  theme: Theme;
  onPress: (symbol: string) => void;
  onLongPress: (symbol: string) => void;
}

// Memoised on the quote object, so a batch that only moves BTC re-renders
// only the BTC row. The flash runs on the native driver, off the JS thread.
function PriceRowImpl({ symbol, quote, alertCount, theme, onPress, onLongPress }: Props) {
  const flash = useRef(new Animated.Value(0)).current;
  const prev = useRef<number | undefined>(undefined);
  const direction = useRef<'up' | 'down'>('up');

  useEffect(() => {
    if (!quote) return;
    if (prev.current !== undefined && quote.p !== prev.current) {
      direction.current = quote.p > prev.current ? 'up' : 'down';
      flash.setValue(1);
      Animated.timing(flash, { toValue: 0, duration: 600, useNativeDriver: true }).start();
    }
    prev.current = quote.p;
  }, [quote, flash]);

  const change = quote ? changeSince(quote.p, quote.o) : undefined;
  const changeColor =
    change === undefined ? theme.muted : change >= 0 ? theme.up : theme.down;
  const flashColor = direction.current === 'up' ? theme.upFlash : theme.downFlash;

  return (
    <Pressable
      onPress={() => onPress(symbol)}
      onLongPress={() => onLongPress(symbol)}
      accessibilityRole="button"
      accessibilityLabel={`${symbol} ${quote ? formatPrice(quote.p) : 'loading'}`}
      accessibilityHint="Tap to set a price alert, long press to remove"
      style={[styles.row, { backgroundColor: theme.card, borderColor: theme.border }]}
    >
      <Animated.View
        pointerEvents="none"
        style={[StyleSheet.absoluteFill, { backgroundColor: flashColor, opacity: flash }]}
      />
      <View style={styles.left}>
        <Text style={[styles.symbol, { color: theme.text }]}>{symbol.split('-')[0]}</Text>
        <Text style={[styles.quote, { color: theme.muted }]}>
          {symbol.split('-')[1]}
          {alertCount > 0 ? `  ·  ${alertCount} alert${alertCount > 1 ? 's' : ''}` : ''}
        </Text>
      </View>
      <View style={styles.right}>
        <Text style={[styles.price, { color: theme.text }]}>
          {quote ? formatPrice(quote.p) : '...'}
        </Text>
        <Text style={[styles.change, { color: changeColor }]}>{formatChange(change)}</Text>
      </View>
    </Pressable>
  );
}

export const PriceRow = memo(PriceRowImpl);

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 16,
    paddingVertical: 14,
    marginHorizontal: 16,
    marginBottom: 8,
    borderRadius: 14,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: 'hidden',
  },
  left: { flex: 1 },
  right: { alignItems: 'flex-end' },
  symbol: { fontSize: 17, fontWeight: '600' },
  quote: { fontSize: 13, marginTop: 2 },
  price: { fontSize: 17, fontWeight: '600', fontVariant: ['tabular-nums'] },
  change: { fontSize: 13, marginTop: 2, fontVariant: ['tabular-nums'] },
});
