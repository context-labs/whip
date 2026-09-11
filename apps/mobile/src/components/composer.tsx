import { Pressable, TextInput, View } from 'react-native';
import { ArrowUp, ChevronDown, Square } from 'lucide-react-native';
import { useDisplay, useTheme } from '../theme/theme';
import { Text, tokens } from '../ui';
export function Composer({ value, onChangeText, onSend, disabled, offline, model, onOptions, onStop }: { value: string; onChangeText(text: string): void; onSend(): void; disabled: boolean; offline: boolean; model: string; onOptions(): void; onStop?: () => void }) {
  const { colors, dark } = useTheme(); const display = useDisplay();
  return <View style={{ borderRadius: tokens.radius.composer, borderWidth: 1, borderColor: colors.border, backgroundColor: colors.panel, padding: 12, gap: 8 }}>
    <TextInput accessibilityLabel="Message" testID="message-input" value={value} onChangeText={onChangeText} multiline placeholder={offline ? 'Draft while disconnected…' : 'Follow up…'} keyboardAppearance={dark ? 'dark' : 'light'} placeholderTextColor={colors.muted} selectionColor={colors.primary} textAlignVertical="top" style={{ minHeight: 48, maxHeight: 160, paddingHorizontal: 4, paddingVertical: 8, color: colors.foreground, fontFamily: display.regular, fontSize: 16 * display.textScale!, lineHeight: 24 * display.textScale! }} />
    <View style={{ flexDirection: 'row', gap: 10, alignItems: 'center' }}><Pressable accessibilityRole="button" accessibilityLabel="Session details" onPress={onOptions} style={{ flex: 1, minHeight: tokens.target, flexDirection: 'row', alignItems: 'center', gap: 6, paddingLeft: 4 }}><Text variant="caption" muted numberOfLines={1} style={{ flexShrink: 1 }}>{model}</Text><ChevronDown size={14} color={colors.muted} /></Pressable>
      {onStop && <Pressable testID="stop-turn" accessibilityRole="button" accessibilityLabel="Stop this turn" onPress={onStop} style={({ pressed }) => ({ width: tokens.target, height: tokens.target, borderRadius: 24, justifyContent: 'center', alignItems: 'center', backgroundColor: pressed ? colors.hover : colors.element })}><Square size={17} fill={colors.foreground} color={colors.foreground} /></Pressable>}
      <Pressable testID="send-message" accessibilityRole="button" accessibilityLabel="Send message" accessibilityState={{ disabled }} disabled={disabled} onPress={onSend} style={({ pressed }) => ({ width: tokens.target, height: tokens.target, borderRadius: 24, justifyContent: 'center', alignItems: 'center', backgroundColor: disabled ? colors.element : colors.primary, opacity: pressed ? 0.7 : 1 })}><ArrowUp size={23} color={disabled ? colors.muted : colors.onPrimary} /></Pressable>
    </View>
  </View>;
}
