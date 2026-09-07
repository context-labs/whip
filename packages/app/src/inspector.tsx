import { Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { inspectorSections, isInspectorSection, type InspectorSection } from './navigation';
import { layout } from './styles';
import { type InspectorProps } from './details/shared';
import { Agents, MailAndState, Executions } from './details/observation';
import { Goals, Limits, ContextSettings, Permissions } from './details/session-controls';
import { Integrations } from './details/integrations';

export function SessionInspector(props: InspectorProps & { section: InspectorSection; onSectionChange(value: InspectorSection): void }) {
  const { section, onSectionChange } = props;
  return (
    <div {...stylex.props(layout.column)}>
      <Select
        label="Inspector section"
        options={inspectorSections}
        value={section}
        onValueChange={value => { if (isInspectorSection(value)) onSectionChange(value); }}
      />
      {!props.connected && (
        <p role="status" {...stylex.props(layout.notice)}>
          Showing retained state. Reconnect to read current host data or apply changes.
        </p>
      )}
      <div
        key={`${props.view.session.client.getSnapshot().info?.runtime_id}:${props.view.session.rootId}:${props.agentId}:${section}`}
        {...stylex.props(layout.column)}
      >
        {section === 'agents' && <Agents {...props} />}
        {section === 'mail' && <MailAndState {...props} />}
        {section === 'execution' && <Executions {...props} />}
        {section === 'goals' && <Goals {...props} />}
        {section === 'limits' && <Limits {...props} />}
        {section === 'context' && <ContextSettings {...props} />}
        {section === 'permissions' && <Permissions {...props} />}
        {section === 'integrations' && <Integrations {...props} />}
      </div>
    </div>
  );
}
