import { Host, Button as NativeButton, Column } from '@expo/ui';
import { ActivityIndicator, Platform, Pressable, ScrollView, StyleSheet, Text, TextInput, View, type TextInputProps, type TextProps, type ViewProps } from 'react-native';
import { useState, type PropsWithChildren } from 'react';
import { useTheme } from '../theme/theme';
import { fonts } from '../theme/fonts';

export function Label({ style, muted, ...props }: TextProps & { muted?: boolean }) {
  const { colors } = useTheme();
  const weight = StyleSheet.flatten(style)?.fontWeight;
  const bold = weight === 'bold' || Number(weight) >= 600;
  return <Text {...props} style={[styles.text, { color: muted ? colors.muted : colors.foreground, fontFamily: bold ? fonts.semibold : fonts.regular }, style, fonts.regular && { fontWeight: 'normal' }]} />;
}
export function Stack({ style, ...props }: ViewProps) { return <View {...props} style={[styles.stack, style]} />; }
export function Screen({ children, scroll = true }: PropsWithChildren<{ scroll?: boolean }>) {
  const theme = useTheme();
  if (!scroll) return <View style={{ flex: 1, backgroundColor: theme.colors.background }}>{children}</View>;
  return <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={styles.page} style={{ flex: 1, backgroundColor: theme.colors.background }}>{children}</ScrollView>;
}
export function Field({ label, style, ...props }: TextInputProps & { label: string }) {
  const { colors } = useTheme();
  return <Stack style={{ gap: 8 }}><Label muted style={styles.caption}>{label}</Label><TextInput
    accessibilityLabel={label} placeholderTextColor={colors.muted} selectionColor={colors.primary}
    {...props} style={[styles.input, { color: colors.foreground, backgroundColor: colors.element, borderColor: colors.border, fontFamily: fonts.regular }, style]} /></Stack>;
}
/** Each action group is a native SwiftUI/Compose host, never a host per transcript row. */
export function Actions({ items }: { items: { label: string; onPress(): void; disabled?: boolean; secondary?: boolean; testID?: string }[] }) {
  const theme = useTheme();
  const [width, setWidth] = useState<number>();
  // Universal native control dimensions must be numeric; percentage styles are
  // accepted by its TS type but crash Compose's integer width modifier.
  return <View onLayout={event => setWidth(Math.floor(event.nativeEvent.layout.width))}><Host matchContents={{ vertical: true }} style={{ width: '100%' }} colorScheme={theme.dark ? 'dark' : 'light'} seedColor={theme.colors.primary}>
    <Column spacing={8}>{items.map(item => <NativeButton key={item.label} label={item.label} testID={item.testID}
      onPress={item.onPress} disabled={item.disabled} variant={item.secondary ? 'outlined' : 'filled'} style={{ paddingVertical: Platform.OS === 'ios' ? 12 : 0, width }} />)}</Column>
  </Host></View>;
}
export function RowButton({ title, detail, onPress, selected, disabled, testID }: { title: string; detail?: string; onPress(): void; selected?: boolean; disabled?: boolean; testID?: string }) {
  const { colors } = useTheme();
  return <Pressable onPress={onPress} accessibilityRole="button" accessibilityState={{ selected, disabled }} disabled={disabled} testID={testID}
    style={({ pressed }) => [styles.row, { backgroundColor: selected ? colors.element : pressed ? colors.hover : 'transparent', borderBottomColor: colors.border, opacity: disabled ? 0.5 : 1 }]}>
    <Label style={{ fontWeight: '600' }}>{title}</Label>{detail && <Label muted style={styles.caption}>{detail}</Label>}
  </Pressable>;
}
export function Notice({ children, danger = false }: PropsWithChildren<{ danger?: boolean }>) {
  const { colors } = useTheme();
  return <View accessibilityLiveRegion="polite" style={[styles.notice, { backgroundColor: colors.panel, borderColor: danger ? colors.error : colors.border }]}><Label style={{ color: danger ? colors.error : colors.muted, fontSize: 14 }}>{children}</Label></View>;
}
export function Loading({ label = 'Loading…' }: { label?: string }) { const theme = useTheme(); return <Stack style={{ padding: 24, alignItems: 'center' }}><ActivityIndicator color={theme.colors.primary} /><Label muted>{label}</Label></Stack>; }
export const styles = StyleSheet.create({
  page: { padding: 20, paddingBottom: 40, gap: 24 },
  stack: { gap: 12 }, text: { fontSize: 16, lineHeight: 24 }, caption: { fontSize: 13, lineHeight: 19 },
  input: { minHeight: 48, borderWidth: StyleSheet.hairlineWidth, borderRadius: 12, paddingHorizontal: 14, paddingVertical: 12, fontSize: 16, lineHeight: 23 },
  row: { paddingVertical: 16, paddingHorizontal: 16, minHeight: 60, gap: 4, borderBottomWidth: StyleSheet.hairlineWidth },
  notice: { padding: 12, borderWidth: StyleSheet.hairlineWidth, borderRadius: 12 },
});
