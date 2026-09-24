import * as stylex from '@stylexjs/stylex';
import { Popover } from '@base-ui/react/popover';
import { useEffect, useId, useRef, useState, type FocusEvent, type KeyboardEvent, type ReactNode, type RefObject } from 'react';
import { useNativeOverlay } from './native-surfaces';
import { styles as shared } from './styles.stylex';
import { colors, scale, surface, typography } from './tokens.stylex';

export interface TextareaSuggestion { value: string; label: string; description?: string }
export interface TextareaSuggestionsOptions {
  open: boolean;
  input: RefObject<HTMLTextAreaElement | null>;
  options: readonly TextareaSuggestion[];
  /** Change when the query/scope changes, even when no options have arrived. */
  queryKey: string;
  label: string;
  status?: string;
  detail?: string;
  feedback?: ReactNode;
  onSelect(value: string): void;
  onDismiss(): void;
}

/** Base UI's Autocomplete input only supports HTMLInputElement. Keep the real
 * multiline textbox and adapt list navigation, not its input/ref contract. */
export function useTextareaSuggestions(props: TextareaSuggestionsOptions) {
  const { open, input, options, queryKey, onSelect, onDismiss } = props;
  const id = useId();
  const [highlight, setHighlight] = useState({ key: queryKey, index: 0 });
  useEffect(() => { setHighlight({ key: queryKey, index: 0 }); }, [queryKey, open]);
  const index = highlight.key === queryKey ? Math.min(highlight.index, options.length - 1) : 0;
  const active = options[index];
  const composing = useRef(false);
  const overlay = useNativeOverlay(open, value => { if (!value) onDismiss(); });
  useEffect(() => {
    if (!overlay.open || !active) return;
    const list = document.getElementById(id);
    const option = document.getElementById(`${id}-${index}`);
    if (!list || !option) return;
    const bounds = list.getBoundingClientRect();
    const row = option.getBoundingClientRect();
    // Scroll only the bounded list, never the transcript or document ancestors.
    if (row.top < bounds.top) list.scrollTop -= bounds.top - row.top;
    else if (row.bottom > bounds.bottom) list.scrollTop += row.bottom - bounds.bottom;
  }, [overlay.open, active, id, index]);
  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>): boolean {
    // Safari can end composition before the confirming keydown; 229 remains an IME key.
    if (composing.current || event.nativeEvent.isComposing || event.keyCode === 229) return true;
    if (!open) return false;
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onDismiss(); return true; }
    if (event.key === 'Tab') { onDismiss(); return true; }
    if (event.shiftKey || event.altKey || event.ctrlKey || event.metaKey) return false;
    if (event.key === 'Enter') {
      event.preventDefault(); event.stopPropagation();
      if (overlay.open && active) onSelect(active.value);
      return true; // Pending, empty, error and native-hide-pending must never submit.
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (options.length) setHighlight({ key: queryKey, index: (index + (event.key === 'ArrowDown' ? 1 : -1) + options.length) % options.length });
      return true;
    }
    return false;
  }
  return {
    onKeyDown,
    inputProps: {
      'aria-autocomplete': 'list' as const,
      'aria-haspopup': 'listbox' as const,
      'aria-controls': overlay.open ? id : undefined,
      'aria-activedescendant': overlay.open && active ? `${id}-${index}` : undefined,
      onBlur: (event: FocusEvent<HTMLTextAreaElement>) => {
        if (!(event.relatedTarget instanceof Node) || !document.getElementById(`${id}-popup`)?.contains(event.relatedTarget)) onDismiss();
      },
      onCompositionStart: () => { composing.current = true; },
      onCompositionEnd: () => { composing.current = false; },
    },
    popup: <Popover.Root {...overlay} modal={false} onOpenChange={(next, details) => {
      if (!next && details.reason === 'outside-press' && details.event.target === input.current) return;
      overlay.onOpenChange(next);
    }}>
      <Popover.Portal>
        <Popover.Positioner anchor={input} side="top" align="start" sideOffset={8} {...stylex.props(shared.positioner)}>
          <Popover.Popup id={`${id}-popup`} initialFocus={false} finalFocus={false} role="presentation" {...stylex.props(shared.popup, styles.popup)}>
            <div {...stylex.props(styles.heading)}>{props.label}</div>
            <div id={id} role="listbox" aria-label={props.label} {...stylex.props(styles.list)}>
              {options.map((option, row) => <div key={option.value} id={`${id}-${row}`} role="option" aria-selected={row === index}
                {...stylex.props(styles.option, row === index && shared.highlighted)}
                onPointerMove={() => setHighlight({ key: queryKey, index: row })}
                onPointerDown={event => event.preventDefault()}
                onClick={() => onSelect(option.value)}>
                <span {...stylex.props(styles.name)}>{option.label}</span>
                {option.description && <span {...stylex.props(styles.description)}>{option.description}</span>}
              </div>)}
            </div>
            {props.status !== undefined && <div role="status" {...stylex.props(styles.status)}>{props.status || '\u00a0'}</div>}
            {props.feedback}
            {props.detail && <div {...stylex.props(styles.status)}>{props.detail}</div>}
            <div {...stylex.props(styles.footer)}>↑↓ Move · Enter Insert · Esc Dismiss</div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>,
  };
}

const styles = stylex.create({
  popup: { width: 'var(--anchor-width)', maxWidth: 'min(560px, calc(100vw - 24px))', minWidth: 0, padding: scale.space1 },
  heading: { padding: scale.space2, fontSize: typography.size12, color: surface.secondaryText, fontWeight: 500 },
  list: { maxHeight: 260, overflowY: 'auto', overscrollBehavior: 'contain' },
  option: { display: 'flex', flexDirection: 'column', gap: scale.space1, padding: scale.space2, borderRadius: scale.radiusSmall, cursor: 'default', overflowWrap: 'anywhere', minHeight: { default: 40, [scale.touch]: 44 } },
  name: { fontFamily: typography.mono, fontSize: typography.size13, color: colors.foreground },
  description: { fontSize: typography.size12, color: surface.secondaryText, lineHeight: 1.5 },
  status: { padding: scale.space2, fontSize: typography.size12, color: surface.secondaryText, overflowWrap: 'anywhere' },
  footer: { borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, padding: scale.space2, fontSize: typography.size12, color: surface.secondaryText },
});
