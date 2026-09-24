module.exports = {
  preset: 'jest-expo',
  moduleNameMapper: {
    '^lucide-react-native$': '<rootDir>/../../node_modules/lucide-react-native/dist/cjs/lucide-react-native.js',
    '^@whip/sdk$': '<rootDir>/../../packages/sdk/dist/index.js',
    '^@whip/sdk/(.*)$': '<rootDir>/../../packages/sdk/dist/$1.js',
    '^@whip/protocol$': '<rootDir>/../../packages/protocol/generated/index.js',
    '^@whip/app/presentation$': '<rootDir>/../../packages/app/src/presentation.ts',
  },
  testMatch: ['<rootDir>/src/**/*.test.ts', '<rootDir>/src/**/*.test.tsx', '<rootDir>/test/**/*.test.ts', '<rootDir>/test/**/*.test.tsx'],
  transform: { '^.+\\.[cm]?[jt]sx?$': ['babel-jest', { presets: ['babel-preset-expo'] }] },
  transformIgnorePatterns: ['node_modules/(?!((jest-)?react-native|@react-native(-community)?|expo(nent)?|@expo(nent)?/.*|expo-.*|@whip/.*|react-native-.*|@shopify/flash-list|lucide-react-native|@tanstack/react-query)/)'],
};
