import { Stack } from 'expo-router';
import { useTheme } from '../../theme/theme';
export default function MainLayout() {
  const { colors } = useTheme();
  return <Stack screenOptions={{ headerStyle: { backgroundColor: colors.background }, headerTintColor: colors.foreground, headerShadowVisible: false, contentStyle: { backgroundColor: colors.background } }}><Stack.Screen name="index" options={{ headerShown: false }} /><Stack.Screen name="settings" options={{ title: 'Settings' }} /><Stack.Screen name="attention" options={{ title: 'Needs you' }} /></Stack>;
}
