import { StyleSheet, Text, View } from 'react-native';

import type { ConnectionStatus } from '../lib/types';
import type { Theme } from '../theme';

const LABEL: Record<ConnectionStatus, string> = {
  idle: 'Paused',
  connecting: 'Connecting',
  open: 'Live',
  reconnecting: 'Reconnecting',
};

export function StatusPill({ status, theme }: { status: ConnectionStatus; theme: Theme }) {
  const color = status === 'open' ? theme.up : status === 'idle' ? theme.muted : theme.warn;
  return (
    <View
      style={[styles.pill, { borderColor: color }]}
      accessibilityLabel={`Connection ${LABEL[status]}`}
    >
      <View style={[styles.dot, { backgroundColor: color }]} />
      <Text style={[styles.text, { color }]}>{LABEL[status]}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  pill: {
    flexDirection: 'row',
    alignItems: 'center',
    borderWidth: 1,
    borderRadius: 999,
    paddingHorizontal: 10,
    paddingVertical: 4,
  },
  dot: { width: 7, height: 7, borderRadius: 4, marginRight: 6 },
  text: { fontSize: 12, fontWeight: '600' },
});
