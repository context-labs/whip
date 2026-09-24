import { Stack as Routes } from 'expo-router';
import licenses from '../../assets/font-licenses.json';
import { Label, Screen, Stack } from '../components/primitives';

export default function LicensesScreen() {
  return <Screen><Routes.Screen options={{ title: 'Font licenses' }} />
    {licenses.map(item => <Stack key={item.name}><Label style={{ fontSize: 22, fontWeight: '600' }}>{item.name}</Label><Label selectable style={{ fontSize: 13, lineHeight: 20 }}>{item.license}</Label></Stack>)}
  </Screen>;
}
