import './polyfills';
import { fetch } from 'expo/fetch';

// The SDK bounds streamed discovery/content reads. React Native's default fetch
// buffers responses and does not provide a reader; Expo supplies that contract.
globalThis.fetch = fetch;
