import { createContext, useCallback, useContext, useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react';
import { useBlocker, type ShouldBlockFn } from '@tanstack/react-router';
import { Button, Dialog } from '@whip/ui';

interface SettingsEdit {
  id: string;
  dirty: boolean;
  description: string;
  discard(): void;
  /** Resolves true only after the host acknowledges the save. Secrets have no implicit save. */
  save?(): Promise<boolean>;
}
type Register = (key: string, edit: SettingsEdit | null) => void;
const SettingsEdits = createContext<Register | null>(null);

/** The route owns the blocker; forms own their ephemeral drafts and explicit save. */
export function SettingsEditsProvider({ children }: { children: ReactNode }) {
  const edits = useRef(new Map<string, SettingsEdit>());
  const [revision, changed] = useState(0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const register = useCallback<Register>((key, edit) => {
    if (edit) edits.current.set(key, edit); else edits.current.delete(key);
    changed(value => value + 1);
  }, []);
  const hasChanges = useCallback(() => [...edits.current.values()].some(edit => edit.dirty), []);
  const shouldBlockFn = useCallback<ShouldBlockFn>(({ current, next }) => {
    const previous = current.search as { section?: unknown; host?: unknown };
    const target = next.search as { section?: unknown; host?: unknown };
    // Linking to another control in the same mounted form cannot lose its draft.
    return (current.pathname !== next.pathname || previous.section !== target.section || previous.host !== target.host) && hasChanges();
  }, [hasChanges]);
  const blocker = useBlocker({ shouldBlockFn, enableBeforeUnload: hasChanges, withResolver: true });
  const pending = useMemo(() => [...edits.current.values()].filter(edit => edit.dirty), [revision]);
  const canSave = pending.length > 0 && pending.every(edit => edit.save);
  useEffect(() => { if (blocker.status === 'idle') setError(''); }, [blocker.status]);
  async function save() {
    if (saving || blocker.status !== 'blocked' || !canSave) return;
    setSaving(true); setError('');
    try {
      for (const edit of pending) if (!await edit.save!()) {
        setError('The changes could not be saved. Stay on this page to review the host error; your edits are preserved.');
        return;
      }
      blocker.proceed();
    } catch (value) { setError(value instanceof Error ? value.message : String(value)); }
    finally { setSaving(false); }
  }
  function discard() {
    if (saving || blocker.status !== 'blocked') return;
    for (const edit of pending) edit.discard();
    blocker.proceed();
  }
  return <SettingsEdits.Provider value={register}>
    {children}
    <Dialog open={blocker.status === 'blocked'} onOpenChange={open => { if (!open && !saving) blocker.reset?.(); }}
      title="Unsaved settings" description="Choose what to do with your changes before leaving this page."
      footer={<>
        <Button variant="ghost" disabled={saving} onClick={() => blocker.reset?.()}>Stay</Button>
        <Button variant="secondary" disabled={saving} onClick={discard}>Discard changes</Button>
        {canSave && <Button variant="primary" loading={saving} onClick={() => void save()}>Save changes</Button>}
      </>}>
      {pending.map(edit => <p key={edit.id}>{edit.description}</p>)}
      {error && <p role="alert">{error}</p>}
    </Dialog>
  </SettingsEdits.Provider>;
}

export function useSettingsEdits(edit: SettingsEdit) {
  const register = useContext(SettingsEdits);
  const key = useId();
  const latest = useRef(edit);
  latest.current = edit;
  // Stable callbacks read the latest form without rerendering the route on each keystroke.
  useEffect(() => {
    if (!register) return;
    register(key, {
      ...latest.current,
      discard: () => latest.current.discard(),
      ...(latest.current.save ? { save: () => latest.current.save!() } : {}),
    });
    return () => register(key, null);
  }, [register, key, edit.dirty, edit.description, Boolean(edit.save)]);
}
