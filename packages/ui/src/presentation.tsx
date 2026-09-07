import * as stylex from '@stylexjs/stylex';
import { Tabs as BaseTabs } from '@base-ui/react/tabs';
import { Collapsible as BaseCollapsible } from '@base-ui/react/collapsible';
import { Accordion as BaseAccordion } from '@base-ui/react/accordion';
import { Progress as BaseProgress } from '@base-ui/react/progress';
import { Meter as BaseMeter } from '@base-ui/react/meter';
import { Avatar as BaseAvatar } from '@base-ui/react/avatar';
import { Separator as BaseSeparator } from '@base-ui/react/separator';
import { Toast as BaseToast } from '@base-ui/react/toast';
import { CSPProvider } from '@base-ui/react/csp-provider';
import { Tooltip as BaseTooltip } from '@base-ui/react/tooltip';
import { mergeProps } from '@base-ui/react/merge-props';
import { AlertCircle, ChevronRight, X } from 'lucide-react';
import type { ComponentPropsWithRef, ReactNode } from 'react';
import { styles } from './styles.stylex';
import { IconButton } from './actions';
import type { Styled } from './actions';

export type Tone = 'neutral' | 'success' | 'warning' | 'error' | 'info';
const toneStyle = {neutral: null, success: styles.success, warning: styles.warning, error: styles.errorTone, info: styles.info};
export function Badge({tone = 'neutral', variant, xstyle, ...props}: ComponentPropsWithRef<'span'> & Styled & {tone?: Tone; variant?: Tone}) {return <span {...mergeProps(stylex.props(styles.badge, toneStyle[variant ?? tone], xstyle), props)}/>;}
export function StatusIndicator({tone = 'neutral', children}: {tone?: Tone; children: ReactNode}) {return <Badge tone={tone}><span aria-hidden {...stylex.props(styles.dot)}/>{children}</Badge>;}
export function Tabs({value, onValueChange, items, label = 'Sections'}: {value: string; onValueChange: (value: string) => void; items: {value: string; label: ReactNode; content: ReactNode; disabled?: boolean}[]; label?: string}) {
  return <BaseTabs.Root value={value} onValueChange={next => onValueChange(String(next))}><BaseTabs.List aria-label={label} {...stylex.props(styles.tabList)}>{items.map(item => <BaseTabs.Tab key={item.value} value={item.value} disabled={item.disabled} className={state => stylex.props(styles.control, styles.button, styles.tab, state.active && styles.tabActive).className}>{item.label}</BaseTabs.Tab>)}</BaseTabs.List>{items.map(item => <BaseTabs.Panel key={item.value} value={item.value} {...stylex.props(styles.tabPanel)}>{item.content}</BaseTabs.Panel>)}</BaseTabs.Root>;
}
export function Collapsible({title, children, open, defaultOpen, onOpenChange}: {title: ReactNode; children: ReactNode; open?: boolean; defaultOpen?: boolean; onOpenChange?: (open: boolean) => void}) {
  return <BaseCollapsible.Root open={open} defaultOpen={defaultOpen} onOpenChange={onOpenChange}><BaseCollapsible.Trigger className={state => stylex.props(styles.control, styles.button, styles.ghost, state.open && styles.tabActive).className}><ChevronRight size={14}/>{title}</BaseCollapsible.Trigger><BaseCollapsible.Panel {...stylex.props(styles.tabPanel)}>{children}</BaseCollapsible.Panel></BaseCollapsible.Root>;
}
export function Accordion({items}: {items: {id: string; title: ReactNode; content: ReactNode}[]}) {
  return <BaseAccordion.Root>{items.map(item => <BaseAccordion.Item key={item.id} value={item.id}><BaseAccordion.Header><BaseAccordion.Trigger {...stylex.props(styles.control, styles.button, styles.ghost)}><ChevronRight size={14}/>{item.title}</BaseAccordion.Trigger></BaseAccordion.Header><BaseAccordion.Panel {...stylex.props(styles.tabPanel)}>{item.content}</BaseAccordion.Panel></BaseAccordion.Item>)}</BaseAccordion.Root>;
}
export function Progress({value, max = 100, label}: {value: number | null; max?: number; label: string}) {
  return <BaseProgress.Root value={value} max={max} {...stylex.props(styles.field)}><BaseProgress.Label {...stylex.props(styles.description)}>{label}</BaseProgress.Label><BaseProgress.Track {...stylex.props(styles.progressTrack)}><BaseProgress.Indicator {...stylex.props(styles.progressFill)}/></BaseProgress.Track></BaseProgress.Root>;
}
export function Meter({value, max = 100, label}: {value: number; max?: number; label: string}) {
  return <BaseMeter.Root value={value} max={max} {...stylex.props(styles.field)}><BaseMeter.Label {...stylex.props(styles.description)}>{label}</BaseMeter.Label><BaseMeter.Track {...stylex.props(styles.progressTrack)}><BaseMeter.Indicator {...stylex.props(styles.progressFill)}/></BaseMeter.Track></BaseMeter.Root>;
}
export function Skeleton({xstyle, ...props}: ComponentPropsWithRef<'div'> & Styled) {return <div aria-hidden {...mergeProps(stylex.props(styles.skeleton, xstyle), props)}/>;}
export function Alert({title, children, tone = 'neutral', action}: {title?: ReactNode; children?: ReactNode; tone?: Tone; action?: ReactNode}) {return <div role={tone === 'error' ? 'alert' : 'status'} {...stylex.props(styles.alert, toneStyle[tone])}><AlertCircle size={16} aria-hidden/><div {...stylex.props(styles.grow)}>{title && <strong>{title}</strong>}{children && <div>{children}</div>}</div>{action}</div>;}
export function EmptyState({title, description, icon, action}: {title: ReactNode; description?: ReactNode; icon?: ReactNode; action?: ReactNode}) {return <div {...stylex.props(styles.empty)}>{icon}<h2 {...stylex.props(styles.emptyTitle)}>{title}</h2>{description && <p {...stylex.props(styles.emptyDescription)}>{description}</p>}{action}</div>;}
export function ErrorState({title = 'Could not load this view', error, action}: {title?: string; error: unknown; action?: ReactNode}) {return <EmptyState title={title} description={error instanceof Error ? error.message : String(error)} icon={<AlertCircle size={24}/>} action={action}/>;}
export function Avatar({name, src}: {name: string; src?: string}) {return <BaseAvatar.Root {...stylex.props(styles.avatar)}>{src && <BaseAvatar.Image src={src} alt={name}/>}<BaseAvatar.Fallback aria-label={name}>{name.trim().slice(0, 2).toUpperCase()}</BaseAvatar.Fallback></BaseAvatar.Root>;}
export function Separator() {return <BaseSeparator {...stylex.props(styles.separator)}/>;}
export function Stack({xstyle, ...props}: ComponentPropsWithRef<'div'> & Styled) {return <div {...mergeProps(stylex.props(styles.stack, xstyle), props)}/>;}
export function Row({xstyle, ...props}: ComponentPropsWithRef<'div'> & Styled) {return <div {...mergeProps(stylex.props(styles.row, xstyle), props)}/>;}
export function Panel({xstyle, ...props}: ComponentPropsWithRef<'section'> & Styled) {return <section {...mergeProps(stylex.props(styles.panel, xstyle), props)}/>;}
export function ScrollArea({xstyle, ...props}: ComponentPropsWithRef<'div'> & Styled) {return <div tabIndex={0} {...mergeProps(stylex.props(styles.scroll, xstyle), props)}/>;}
export function SettingsRow({label, description, children}: {label: ReactNode; description?: ReactNode; children: ReactNode}) {return <div {...stylex.props(styles.settingsRow)}><div {...stylex.props(styles.grow)}><div {...stylex.props(styles.label)}>{label}</div>{description && <p {...stylex.props(styles.description)}>{description}</p>}</div>{children}</div>;}
export function Breadcrumbs({children, label = 'Location'}: {children: ReactNode; label?: string}) {return <nav aria-label={label} {...stylex.props(styles.row)}>{children}</nav>;}
export function VisuallyHidden({children}: {children: ReactNode}) {return <span {...stylex.props(styles.visuallyHidden)}>{children}</span>;}

const toastStyles = stylex.create({
  viewport: {position: 'fixed', bottom: 20, right: 20, zIndex: 150, width: 'min(380px, calc(100vw - 40px))', display: 'flex', flexDirection: 'column', gap: 8},
});
export function UIProvider({children}: {children: ReactNode}) {return <CSPProvider disableStyleElements><BaseTooltip.Provider delay={400}><BaseToast.Provider timeout={5000}>{children}<ToastViewport/></BaseToast.Provider></BaseTooltip.Provider></CSPProvider>;}
function ToastViewport() {
  const {toasts} = BaseToast.useToastManager();
  return <BaseToast.Portal><BaseToast.Viewport {...stylex.props(toastStyles.viewport)}>{toasts.map(toast => <BaseToast.Root key={toast.id} toast={toast} {...stylex.props(styles.alert)}><BaseToast.Content {...stylex.props(styles.grow)}><BaseToast.Title {...stylex.props(styles.label)}/><BaseToast.Description {...stylex.props(styles.description)}/></BaseToast.Content><BaseToast.Close render={<IconButton label="Dismiss notification" variant="ghost"><X size={14}/></IconButton>}/></BaseToast.Root>)}</BaseToast.Viewport></BaseToast.Portal>;
}
export function useToast() {return BaseToast.useToastManager();}
