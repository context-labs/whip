import { Tabs } from 'expo-router';
import { Bell, MessageSquare, Settings } from 'lucide-react-native';
import { useTheme } from '../../theme/theme';
import { useAttention } from '../../features/attention';
export default function TabLayout() {
  const { colors } = useTheme();
  const attention = useAttention();
  const needsAttention = attention.status === 'current' && attention.count > 0;
  return <Tabs screenOptions={{ headerStyle: { backgroundColor: colors.background }, headerTintColor: colors.foreground, headerShadowVisible: false, tabBarStyle: { backgroundColor: colors.panel, borderTopColor: colors.border }, tabBarActiveTintColor: colors.primary, tabBarInactiveTintColor: colors.muted }}>
    <Tabs.Screen name="index" options={{ title: 'Sessions', tabBarIcon: ({ color, size }) => <MessageSquare color={color} size={size} /> }} />
    <Tabs.Screen name="attention" options={{ title: 'Attention', tabBarBadge: attention.badge, tabBarBadgeStyle: { backgroundColor: needsAttention ? colors.warning : colors.element, color: needsAttention ? colors.background : colors.muted }, tabBarAccessibilityLabel: attention.accessibilityLabel, tabBarIcon: ({ color, size }) => <Bell color={color} size={size} /> }} />
    <Tabs.Screen name="settings" options={{ title: 'Settings', tabBarIcon: ({ color, size }) => <Settings color={color} size={size} /> }} />
  </Tabs>;
}
