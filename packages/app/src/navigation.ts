/** Shared destinations for routing, inspector navigation and the command picker. */
export const inspectorSections = [
  { value: 'agents', label: 'Agent tree' },
  { value: 'mail', label: 'Mail & shared state' },
  { value: 'execution', label: 'Executions' },
  { value: 'goals', label: 'Goals & schedules' },
  { value: 'limits', label: 'Usage & authority' },
  { value: 'context', label: 'Context & model' },
  { value: 'permissions', label: 'Permissions' },
  { value: 'integrations', label: 'Host integrations' },
] as const;
export type InspectorSection = typeof inspectorSections[number]['value'];
export const isInspectorSection = (value: unknown): value is InspectorSection => inspectorSections.some(item => item.value === value);
