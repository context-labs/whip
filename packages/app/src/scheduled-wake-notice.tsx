import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import type { DeepReadonly, SessionViewSnapshot } from '@whip/sdk/state';
import { Button } from '@whip/ui';
import { ChevronDown, ChevronRight, Clock } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';

type Wake = NonNullable<NonNullable<SessionViewSnapshot['root']>['upcoming_schedules']>[number];

/** Calendar comparisons and display both use the viewer's timezone, not UTC. */
export function wakeTimestamp(value: string, now = new Date(), locale?: string, timeZone?: string) {
  const date = new Date(value);
  const day = new Intl.DateTimeFormat(locale, { year: 'numeric', month: 'numeric', day: 'numeric', timeZone });
  const sameDay = day.format(date) === day.format(now);
  return {
    label: new Intl.DateTimeFormat(locale, {
      ...(!sameDay ? { month: 'short' as const, day: 'numeric' as const, year: 'numeric' as const } : {}),
      hour: 'numeric', minute: '2-digit', timeZoneName: 'short', timeZone,
    }).format(date),
    full: new Intl.DateTimeFormat(locale, { dateStyle: 'full', timeStyle: 'long', timeZone }).format(date),
    overdue: date.getTime() < now.getTime(),
  };
}

function WakeTime({ wake, now }: { wake: DeepReadonly<Wake>; now: Date }) {
  const time = wakeTimestamp(wake.next_fire, now);
  return <>{time.overdue ? 'Scheduled wake-up was due at ' : 'This session is scheduled to wake up at '}
    <time dateTime={wake.next_fire} title={time.full} aria-label={time.full}>{time.label}</time>
  </>;
}

/** This is a display of fresh SDK evidence, never an inbox or a client scheduler. */
export function ScheduledWakeNotice({ state, agentId, connected, onSchedules }: {
  state: DeepReadonly<SessionViewSnapshot>;
  agentId: string;
  connected: boolean;
  onSchedules(): void;
}) {
  const contentId = useId();
  const heading = useRef<HTMLButtonElement>(null);
  const contentFocused = useRef(false);
  const [expandedOccurrence, setExpandedOccurrence] = useState<string>();
  const root = state.root;
  const supported = connected && state.status === 'live' && root?.root_id === agentId && root.upcoming_schedules != null;
  const wakes = supported ? root.upcoming_schedules! : [];
  const partial = supported && !!root.omitted?.upcoming_schedules;
  const total = supported ? root.upcoming_schedule_count : undefined;
  const first = wakes[0];
  const [clock, setClock] = useState(Date.now);
  const ticking = !!first;
  // Local time only refreshes labels; the host alone claims or advances occurrences.
  useEffect(() => {
    if (!ticking) return;
    let timer: ReturnType<typeof setInterval> | undefined;
    const update = () => {
      clearInterval(timer);
      if (!document.hidden) {
        setClock(Date.now());
        timer = setInterval(() => setClock(Date.now()), 1000);
      }
    };
    update();
    document.addEventListener('visibilitychange', update);
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', update); };
  }, [ticking]);
  const occurrence = first ? JSON.stringify([root!.root_id, first.id, first.next_fire])
    : partial && total != null && total > 0 ? JSON.stringify([root!.root_id, 'partial']) : undefined;
  const expanded = !!occurrence && expandedOccurrence === occurrence;
  // A replaced occurrence starts collapsed without replacing the focused header.
  useLayoutEffect(() => {
    if (!expanded && contentFocused.current) {
      contentFocused.current = false;
      if (occurrence) heading.current?.focus({ preventScroll: true });
    }
  }, [expanded, occurrence]);
  if (!occurrence) return null;
  const more = total != null ? Math.max(0, total - 1) : partial ? undefined : wakes.length - 1;
  const now = new Date(clock);
  return <section aria-label="Scheduled wake-ups" data-scheduled-wake {...stylex.props(styles.notice)}>
    <button ref={heading} type="button" {...stylex.props(styles.heading)}
      aria-expanded={expanded} aria-controls={contentId}
      onClick={() => setExpandedOccurrence(expanded ? undefined : occurrence)}>
      {expanded ? <ChevronDown size={14} aria-hidden="true" {...stylex.props(styles.icon)} /> : <ChevronRight size={14} aria-hidden="true" {...stylex.props(styles.icon)} />}
      <Clock size={14} aria-hidden="true" {...stylex.props(styles.icon)} />
      <span {...stylex.props(styles.summary)}>{first ? <>
        <WakeTime wake={first} now={now} />
        {more === undefined ? ' · schedule details incomplete' : more > 0 ? ` · +${more} more` : ''}
      </> : `This session has ${total} scheduled wake-up${total === 1 ? '' : 's'}`}
      </span>
    </button>
    <div id={contentId} hidden={!expanded}
      onFocusCapture={() => { contentFocused.current = true; }}
      onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget)) contentFocused.current = false; }}>
      {expanded && <div role="region" aria-label="Scheduled messages" tabIndex={0} {...stylex.props(styles.messages)}>
        {wakes.map(wake => <div key={JSON.stringify([root!.root_id, wake.id, wake.next_fire])} {...stylex.props(styles.message)}>
          {wakes.length > 1 && <p {...stylex.props(styles.label)}><WakeTime wake={wake} now={now} /></p>}
          <p {...stylex.props(styles.label)}>{wake.prompt_truncated ? 'Message preview' : 'Message to be sent'}</p>
          <p {...stylex.props(styles.prompt)}>{wake.prompt}</p>
          {wake.prompt_truncated && <p {...stylex.props(styles.label)}>Prompt preview is incomplete. Open schedules to read the full message.</p>}
        </div>)}
        {partial && <p {...stylex.props(styles.label)}>Partial schedule list. Open schedules to view more.</p>}
        {(partial || wakes.some(wake => wake.prompt_truncated)) && <Button type="button" variant="ghost" size="sm" onClick={onSchedules}>Open schedules</Button>}
      </div>}
    </div>
  </section>;
}

const styles = stylex.create({
  notice: { minWidth: 0, marginBottom: 4, color: surface.secondaryText },
  heading: {
    backgroundColor: 'transparent', borderWidth: 0, borderRadius: scale.radiusControl,
    fontFamily: 'inherit', cursor: 'pointer', outlineOffset: 2, display: 'flex', minWidth: 0,
    width: '100%', height: 'auto', minHeight: { default: 32, [scale.touch]: 44 },
    paddingBlock: 6, paddingInline: 8, gap: 6, justifyContent: 'flex-start', alignItems: 'flex-start',
    fontSize: typography.size12, fontWeight: 400, lineHeight: 1.5, color: surface.secondaryText,
    textAlign: 'start', whiteSpace: 'normal',
  },
  icon: { flexShrink: 0, marginTop: 2 },
  summary: { minWidth: 0, overflowWrap: 'anywhere' },
  messages: {
    maxHeight: 'min(200px, 20dvh)', overflowY: 'auto', overscrollBehavior: 'contain',
    scrollbarGutter: 'stable', paddingBlock: 8, paddingInline: 12, marginInline: 8,
    borderInlineStartWidth: 1, borderInlineStartStyle: 'solid', borderInlineStartColor: surface.quietBorder,
  },
  message: { minWidth: 0, marginBottom: 12 },
  label: { marginBlock: 4, fontSize: typography.size12, color: surface.secondaryText },
  prompt: { marginBlock: 4, fontSize: typography.size14, color: colors.foreground, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', userSelect: 'text' },
});
