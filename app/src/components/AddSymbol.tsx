import { useState } from 'react';
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native';

import { normalizeSymbol } from '../lib/format';
import type { Theme } from '../theme';

interface Props {
  theme: Theme;
  existing: string[];
  onAdd: (symbol: string) => void;
}

export function AddSymbol({ theme, existing, onAdd }: Props) {
  const [text, setText] = useState('');
  const [hint, setHint] = useState<string | null>(null);

  const submit = () => {
    const sym = normalizeSymbol(text);
    if (!sym) {
      setHint('Try BTC, ETH-USD or SOL-USDT');
      return;
    }
    if (existing.includes(sym)) {
      setHint(`${sym} is already on your list`);
      return;
    }
    onAdd(sym);
    setText('');
    setHint(null);
  };

  return (
    <View style={styles.wrap}>
      <View style={[styles.bar, { backgroundColor: theme.card, borderColor: theme.border }]}>
        <TextInput
          value={text}
          onChangeText={(t) => {
            setText(t);
            setHint(null);
          }}
          onSubmitEditing={submit}
          placeholder="Add a market, e.g. ETH or DOGE-USD"
          placeholderTextColor={theme.muted}
          autoCapitalize="characters"
          autoCorrect={false}
          returnKeyType="done"
          style={[styles.input, { color: theme.text }]}
          accessibilityLabel="Add a market"
        />
        <Pressable onPress={submit} hitSlop={8} accessibilityRole="button">
          <Text style={[styles.add, { color: theme.accent }]}>Add</Text>
        </Pressable>
      </View>
      {hint ? <Text style={[styles.hint, { color: theme.warn }]}>{hint}</Text> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { marginHorizontal: 16, marginBottom: 12 },
  bar: {
    flexDirection: 'row',
    alignItems: 'center',
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 14,
  },
  input: { flex: 1, paddingVertical: 12, fontSize: 15 },
  add: { fontSize: 15, fontWeight: '600', paddingLeft: 12 },
  hint: { fontSize: 13, marginTop: 6, marginLeft: 4 },
});
