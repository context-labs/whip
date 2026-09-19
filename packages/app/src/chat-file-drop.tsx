import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type Ref, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';

const styles = stylex.create({
  surface: { position: 'relative', display: 'flex', flexDirection: 'column', flex: '1 1 0', minWidth: 0, minHeight: 0 },
  overlay: {
    position: 'absolute', inset: 0, zIndex: 5, pointerEvents: 'none',
    display: 'grid', placeItems: 'center', padding: scale.space4,
    backgroundColor: `color-mix(in srgb, ${colors.borderFocus} 12%, transparent)`,
    boxShadow: `inset 0 0 0 1px ${colors.borderFocus}`,
  },
  unavailable: { backgroundColor: `color-mix(in srgb, ${colors.background} 65%, transparent)`, boxShadow: `inset 0 0 0 1px ${surface.quietBorder}` },
  label: {
    maxWidth: '100%', paddingBlock: scale.space2, paddingInline: scale.space3,
    borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder,
    borderRadius: scale.radiusControl, backgroundColor: colors.element, color: colors.foreground,
    fontSize: typography.size13, textAlign: 'center', overflowWrap: 'anywhere',
  },
});

export function ChatDropSurface({ children, ref }: { children: ReactNode; ref: Ref<HTMLDivElement> }) {
  return <div ref={ref} data-chat-drop-surface {...stylex.props(styles.surface)}>{children}</div>;
}

function hasFiles(transfer: DataTransfer | null) {
  return !!transfer && (Array.from(transfer.types ?? []).includes('Files')
    || Array.from(transfer.items ?? []).some(item => item.kind === 'file') || transfer.files?.length > 0);
}

/** Never navigate the application when a file misses an available attachment target. */
export function FileDropNavigationGuard() {
  useEffect(() => {
    const preventNavigation = (event: DragEvent) => {
      if (!hasFiles(event.dataTransfer) || event.defaultPrevented) return;
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = 'none';
    };
    window.addEventListener('dragover', preventNavigation);
    window.addEventListener('drop', preventNavigation);
    return () => {
      window.removeEventListener('dragover', preventNavigation);
      window.removeEventListener('drop', preventNavigation);
    };
  }, []);
  return null;
}

/** Native listeners follow the pane's DOM boundary, excluding portaled dialogs. */
export function ChatFileDrop({ target, scope, unavailable, onFiles, onError }: {
  target: RefObject<HTMLElement | null>;
  scope: string;
  unavailable?: string;
  onFiles(files: File[]): void | Promise<void>;
  onError(message: string): void;
}) {
  const current = useRef({ unavailable, onFiles, onError });
  useLayoutEffect(() => { current.current = { unavailable, onFiles, onError }; });
  const [hover, setHover] = useState<{ element: HTMLElement; scope: string }>();
  useEffect(() => {
    const element = target.current;
    if (!element) return;
    let depth = 0;
    let visible = false;
    let expiry: ReturnType<typeof setTimeout> | undefined;
    const clear = () => {
      depth = 0;
      clearTimeout(expiry);
      if (visible) { visible = false; setHover(undefined); }
    };
    const inside = (event: DragEvent) => event.target instanceof Node && element.contains(event.target)
      && !element.closest('[inert]') && !document.querySelector('[aria-modal="true"]');
    const show = (event: DragEvent) => {
      if (!hasFiles(event.dataTransfer) || !inside(event)) return;
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = current.current.unavailable ? 'none' : 'copy';
      if (!visible) { visible = true; setHover({ element, scope }); }
      // OS drags do not fire dragend, and Escape may not reach the page. Native
      // dragover keeps this one-shot expiry alive, including a stationary cursor.
      clearTimeout(expiry);
      expiry = setTimeout(clear, 1500);
    };
    const enter = (event: DragEvent) => {
      if (!hasFiles(event.dataTransfer) || !inside(event)) return;
      depth++;
      show(event);
    };
    const leave = (event: DragEvent) => {
      if (!visible) return;
      depth = Math.max(0, depth - 1);
      if (!depth && !(event.relatedTarget instanceof Node && element.contains(event.relatedTarget))) clear();
    };
    const drop = (event: DragEvent) => {
      if (!hasFiles(event.dataTransfer) || !inside(event)) return;
      event.preventDefault();
      event.stopPropagation();
      clear();
      const { unavailable, onFiles, onError } = current.current;
      if (unavailable) { onError(unavailable); return; }
      const transfer = event.dataTransfer!;
      if (Array.from(transfer.items ?? []).some(item => item.kind === 'file' && item.webkitGetAsEntry?.()?.isDirectory)) {
        onError('Drop individual files to attach them. Folders are not supported.');
        return;
      }
      const files = Array.from(transfer.files);
      if (!files.length) { onError('These files could not be read. Try the Attach button.'); return; }
      try { Promise.resolve(onFiles(files)).catch(error => onError(error instanceof Error ? error.message : String(error))); }
      catch (error) { onError(error instanceof Error ? error.message : String(error)); }
    };
    const outside = (event: DragEvent) => { if (!inside(event)) clear(); };
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') clear(); };
    const hidden = () => { if (document.hidden) clear(); };
    element.addEventListener('dragenter', enter);
    element.addEventListener('dragover', show);
    element.addEventListener('dragleave', leave);
    element.addEventListener('drop', drop);
    window.addEventListener('dragenter', outside, true);
    window.addEventListener('dragover', outside, true);
    window.addEventListener('drop', clear, true);
    window.addEventListener('dragend', clear);
    window.addEventListener('blur', clear);
    window.addEventListener('keydown', escape, true);
    document.addEventListener('visibilitychange', hidden);
    return () => {
      clear();
      element.removeEventListener('dragenter', enter);
      element.removeEventListener('dragover', show);
      element.removeEventListener('dragleave', leave);
      element.removeEventListener('drop', drop);
      window.removeEventListener('dragenter', outside, true);
      window.removeEventListener('dragover', outside, true);
      window.removeEventListener('drop', clear, true);
      window.removeEventListener('dragend', clear);
      window.removeEventListener('blur', clear);
      window.removeEventListener('keydown', escape, true);
      document.removeEventListener('visibilitychange', hidden);
    };
  }, [target, scope]);
  return hover?.scope === scope ? createPortal(
    <div data-chat-file-drop {...stylex.props(styles.overlay, !!unavailable && styles.unavailable)}>
      <span role="status" {...stylex.props(styles.label)}>{unavailable ?? 'Drop to attach'}</span>
    </div>, hover.element,
  ) : null;
}
