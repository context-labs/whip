import { loadAsync } from 'expo-font';

// Import individual assets; the font package index includes every weight.
export const fonts: { regular?: string; semibold?: string; mono?: string } = {};
export async function loadFonts() {
  try {
    await loadAsync({
      'Inter-Regular': require('@expo-google-fonts/inter/400Regular/Inter_400Regular.ttf'),
      'Inter-SemiBold': require('@expo-google-fonts/inter/600SemiBold/Inter_600SemiBold.ttf'),
      'JetBrainsMono-Regular': require('@expo-google-fonts/jetbrains-mono/400Regular/JetBrainsMono_400Regular.ttf'),
    });
    Object.assign(fonts, { regular: 'Inter-Regular', semibold: 'Inter-SemiBold', mono: 'JetBrainsMono-Regular' });
  } catch {
    // System fonts keep connection/recovery usable if an asset cannot load.
  }
}
