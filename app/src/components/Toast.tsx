import { useEffect, useRef } from 'react';
import { Animated, StyleSheet, Text } from 'react-native';

import type { Theme } from '../theme';

// A short banner for fired alerts and connection errors.
export function Toast({
  message,
  theme,
  onHide,
}: {
  message: string | null;
  theme: Theme;
  onHide: () => void;
}) {
  const opacity = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    if (!message) return;
    opacity.setValue(0);
    Animated.sequence([
      Animated.timing(opacity, { toValue: 1, duration: 180, useNativeDriver: true }),
      Animated.delay(3500),
      Animated.timing(opacity, { toValue: 0, duration: 250, useNativeDriver: true }),
    ]).start(({ finished }) => finished && onHide());
  }, [message, opacity, onHide]);

  if (!message) return null;
  return (
    <Animated.View
      pointerEvents="none"
      accessibilityLiveRegion="polite"
      style={[styles.toast, { backgroundColor: theme.text, opacity }]}
    >
      <Text style={[styles.text, { color: theme.bg }]}>{message}</Text>
    </Animated.View>
  );
}

const styles = StyleSheet.create({
  toast: {
    position: 'absolute',
    left: 16,
    right: 16,
    bottom: 24,
    borderRadius: 12,
    paddingVertical: 12,
    paddingHorizontal: 16,
  },
  text: { fontSize: 15, fontWeight: '600', textAlign: 'center' },
});
