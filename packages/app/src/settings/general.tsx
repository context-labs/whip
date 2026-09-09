import { Select, Switch } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime } from '../context';
import { commandShortcuts, composerShortcuts } from '../runtime';
import { layout } from '../styles';
import { SettingsGroup, SettingRow, settingsSection } from './section-layout';

export function GeneralSettings() {
  const runtime = useRuntime();
  const { preferences } = useAppState();
  function update(patch: Parameters<typeof runtime.setPreferences>[0]) {
    try { runtime.setPreferences(patch); } catch (error) { runtime.report(error); }
  }
  return <>
    <SettingsGroup title="Keyboard shortcuts">
      <SettingRow id="commandShortcut" label="Open commands" description="Open the command palette. Mod means Command on macOS and Control elsewhere.">
        <Select label="Open commands" value={preferences.commandShortcut} xstyle={settingsSection.control}
          options={commandShortcuts.map(value => ({ value, label: value }))}
          onValueChange={value => update({ commandShortcut: value as typeof preferences.commandShortcut })} />
      </SettingRow>
      <SettingRow id="composerShortcut" label="Focus message composer" description="Jump to the message field in your active conversation.">
        <Select label="Focus message composer" value={preferences.composerShortcut} xstyle={settingsSection.control}
          options={composerShortcuts.map(value => ({ value, label: value }))}
          onValueChange={value => update({ composerShortcut: value as typeof preferences.composerShortcut })} />
      </SettingRow>
      <p {...stylex.props(layout.muted)}>Enter sends a message. Shift + Enter inserts a line. Escape closes a menu or dialog. These preferences are saved on this device.</p>
    </SettingsGroup>
    <SettingsGroup title="Attention">
      <SettingRow id="attentionAnnouncements" label="Announce attention changes" description="Politely announce sessions needing a response to assistive technology. The visual attention indicator always remains available.">
        <Switch aria-label="Announce attention changes" checked={preferences.attentionAnnouncements} onCheckedChange={attentionAnnouncements => update({ attentionAnnouncements })} />
      </SettingRow>
      {runtime.platform.notify && <SettingRow id="desktopNotifications" label="Desktop notifications" description="Notify me about new permission requests and questions while Whip is open, including when its window is hidden. Checks up to 256 active sessions per connected host. Notifications stop when Whip quits.">
        <Switch aria-label="Desktop notifications" checked={preferences.desktopNotifications} onCheckedChange={desktopNotifications => update({ desktopNotifications })} />
      </SettingRow>}
    </SettingsGroup>
  </>;
}
