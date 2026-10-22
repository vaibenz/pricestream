// Gateway WebSocket URL. Set EXPO_PUBLIC_GATEWAY_URL when starting Expo, for
// example to your machine's LAN address when testing on a phone:
//   EXPO_PUBLIC_GATEWAY_URL=ws://192.168.1.20:8080/ws npx expo start
// The Android emulator reaches the host machine at 10.0.2.2, not localhost.
export const GATEWAY_URL = process.env.EXPO_PUBLIC_GATEWAY_URL ?? 'ws://localhost:8080/ws';
