import { assertValid, type InitializeParams, type SubscribeParams } from './generated/index.js';
const initialize: InitializeParams = { protocol_major: 2, build_id: 'fixture', client_kind: 'human', client_id: 'browser' };
const subscription: SubscribeParams = { root_id: 'root', subscription_id: 'view', cursor: '9007199254740993' };
assertValid('InitializeParams', initialize);
assertValid('SubscribeParams', subscription);
