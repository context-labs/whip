import type { ExpoConfig } from 'expo/config';

const config: ExpoConfig = {
  name: 'Whip',
  slug: 'whipcode',
  owner: 'inference',
  version: '0.1.0',
  scheme: 'whip',
  orientation: 'default',
  userInterfaceStyle: 'automatic',
  icon: './assets/icon.png',
  ios: {
    bundleIdentifier: process.env.WHIP_MOBILE_BUNDLE_ID ?? 'dev.contextlabs.whip.mobile',
    supportsTablet: true,
    infoPlist: {
      NSLocalNetworkUsageDescription: 'Connect to your Whip host over your private network.',
    },
  },
  android: {
    package: process.env.WHIP_MOBILE_BUNDLE_ID ?? 'dev.contextlabs.whip.mobile',
    allowBackup: false,
    predictiveBackGestureEnabled: true,
    adaptiveIcon: { foregroundImage: './assets/android-icon-foreground.png', monochromeImage: './assets/android-icon-monochrome.png', backgroundColor: '#141414' },
  },
  plugins: [
    'expo-router',
    ['expo-sqlite', { useSQLCipher: true }],
    ['expo-secure-store', { configureAndroidBackup: true }],
    'expo-font',
    ['expo-splash-screen', { backgroundColor: '#141414', image: './assets/splash-icon.png', imageWidth: 180 }],
  ],
  experiments: { typedRoutes: true },
  extra: { eas: { projectId: 'fa9874ce-4324-474f-86ef-a8749cf8fa91' } },
};
export default config;
