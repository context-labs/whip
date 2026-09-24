// Existing feature views use these names while composing the native library.
import { StyleSheet } from 'react-native';
import { Button, Stack } from '../ui';
export { Text as Label, TextField as Field, ListRow as RowButton, Stack, Screen, Notice, Loading } from '../ui';
export function Actions({ items }: { items: { label: string; onPress(): void; disabled?: boolean; secondary?: boolean; testID?: string }[] }) {
  return <Stack style={{ gap: 8 }}>{items.map(item => <Button key={item.label} {...item} variant={item.secondary ? 'secondary' : 'primary'} />)}</Stack>;
}
export const styles = StyleSheet.create({ caption: { fontSize: 13, lineHeight: 19 }, text: { fontSize: 16, lineHeight: 24 }, page: { padding: 20, gap: 24 }, stack: { gap: 12 }, input: { minHeight: 52, borderRadius: 12, padding: 12, fontSize: 16, lineHeight: 24 }, row: { minHeight: 68, padding: 16 }, notice: { padding: 14, borderRadius: 12 } });
