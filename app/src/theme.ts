import { useColorScheme } from 'react-native';

const light = {
  bg: '#F6F7F9',
  card: '#FFFFFF',
  text: '#0B0E14',
  muted: '#5B6472',
  border: '#E3E6EB',
  up: '#0A8F4D',
  down: '#D33A2C',
  upFlash: 'rgba(10,143,77,0.14)',
  downFlash: 'rgba(211,58,44,0.14)',
  accent: '#1652F0',
  warn: '#B25E00',
};

const dark: typeof light = {
  bg: '#0B0E14',
  card: '#141821',
  text: '#F2F4F7',
  muted: '#8A93A3',
  border: '#232937',
  up: '#2DD07C',
  down: '#FF6B5E',
  upFlash: 'rgba(45,208,124,0.16)',
  downFlash: 'rgba(255,107,94,0.16)',
  accent: '#5B8CFF',
  warn: '#F2A93B',
};

export type Theme = typeof light;

export function useTheme(): Theme {
  return useColorScheme() === 'dark' ? dark : light;
}
