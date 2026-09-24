// beautiful-mermaid 1.1.3 / elkjs 0.11.1's FakeWorker detection is environment
// sensitive. ponytail: remove this release-specific shim when upstream fixes worker
// initialization. In this dedicated worker only, select CJS and allow upstream's self
// restoration through its one-time warmup. Restore both descriptors immediately.
const scope = globalThis as unknown as Record<string, unknown>;
const documentDescriptor = Object.getOwnPropertyDescriptor(scope, 'document');
const selfDescriptor = Object.getOwnPropertyDescriptor(scope, 'self');
const setTimeoutBeforeInit = globalThis.setTimeout;
if (!documentDescriptor) scope.document = {};
Object.defineProperty(scope, 'self', {value: globalThis, writable: true, configurable: true});
export function restoreWorkerScope() {
  globalThis.setTimeout = setTimeoutBeforeInit;
  if (!documentDescriptor) delete scope.document;
  if (selfDescriptor) Object.defineProperty(scope, 'self', selfDescriptor);
  else delete scope.self;
}
