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
import { colors } from './tokens.stylex';
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
/**
 * The Whipcode wordmark (source: whipcode-wordmark.svg, viewBox 2008x395).
 * Paths fill with currentColor so the mark follows the active theme's text
 * color on any surface; size it with fontSize or an explicit width.
 */
export function WhipcodeWordmark({xstyle, ...props}: Omit<ComponentPropsWithRef<'svg'>, 'children' | 'viewBox'> & Styled) {
  return <svg viewBox="0 0 2008 395" fill="none" xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" role="img" {...mergeProps(stylex.props(wordmarkStyle.mark, xstyle), props)}>
    <path d="M653 0.812743L615.5 1.31274V38.8127V76.3127L656.2 76.6127L696.9 76.8127L697.9 75.0127L698.9 73.1127L698.5 37.8127L698 2.51274L696.9 1.21274C696.3 0.412743 694.6 -0.087257 693.1 0.012743C691.7 0.212743 673.6 0.512743 653 0.812743Z" fill="currentColor"/>
    <path d="M1667.1 2.21274L1665.9 3.61274L1666 48.8127C1666.1 73.6127 1666 94.2127 1665.8 94.4127C1665.6 94.6127 1636.3 95.1127 1600.8 95.4127L1536.2 96.1127L1521.3 111.013L1506.5 125.813V202.313V278.813L1523.1 295.313L1539.6 311.813H1648.7H1757.9L1759.4 310.213L1761 308.713V269.013C1761 247.213 1761.3 184.213 1761.7 129.013L1762.3 28.7127L1760 25.5127C1758.7 23.7127 1753.1 17.5127 1747.6 11.5127L1737.7 0.81274H1703H1668.2L1667.1 2.21274ZM1666 205.313V244.813H1630.5H1595V205.313V165.813H1630.5H1666V205.313Z" fill="currentColor"/>
    <path d="M353 4.71274L342.5 5.31274V41.8127C342.6 61.9127 342.7 130.013 342.8 193.213L343 308.113L344.1 309.513L345.2 310.813H392.1H438.9L440 308.913L441 306.913V236.413V165.813H471.5H502V237.113V308.413L503.2 309.613L504.4 310.813H550.6H596.8L597.8 309.513L598.9 308.213L599.1 218.013L599.4 127.713L583.4 111.713L567.5 95.8127H521.5C496.2 95.8127 468.5 95.5127 459.9 95.1127L444.3 94.5127L443.7 63.1127L443.1 31.7127L430 17.8127L416.8 3.81274L390.1 4.01274C375.5 4.11274 358.8 4.41274 353 4.71274Z" fill="currentColor"/>
    <path d="M1877 94.7128L1807.5 95.3128L1805.5 96.5128C1804.4 97.1128 1797.3 103.913 1789.7 111.713L1776 125.713V165.013C1776 186.613 1776 221.413 1776 242.313V280.313L1791.5 295.813L1807 311.313H1890.5H1974L1989 296.313L2004 281.313V275.813V270.313L1992.3 258.613L1980.5 246.913L1924.5 246.813C1893.7 246.713 1867 246.413 1865.3 246.113L1862 245.613V236.913V228.313L1914.8 227.913C1943.8 227.713 1976 227.313 1986.4 226.913L2005.2 226.213L2006.6 224.313L2008 222.413V174.313V126.313L1992 110.313L1976 94.3128L1961.2 94.2128C1953.1 94.1128 1915.2 94.4128 1877 94.7128ZM1921 168.313V178.813L1913.3 178.913C1909 178.913 1895.7 179.013 1883.7 179.113L1862 179.313V168.513V157.813H1891.5H1921V168.313Z" fill="currentColor"/>
    <path d="M626.3 95.5127L617.2 95.9127L616.1 97.2127L615 98.5127L614.7 204.413L614.5 310.313L658.4 310.613L702.3 310.813L703.6 309.713L705 308.613V214.513V120.513L692.7 108.113L680.4 95.7127L658 95.4127C645.6 95.2127 631.4 95.2127 626.3 95.5127Z" fill="currentColor"/>
    <path d="M734.4 110.113L720.2 124.513L719.5 170.413C718.7 215.313 719.6 392.413 720.6 394.013C720.9 394.413 732.7 394.813 746.8 394.813H772.5L789.8 377.513L807 360.313V351.213C807 346.213 807.3 335.313 807.7 327.013L808.3 311.813L867.7 311.613L927 311.313L945.4 292.813L963.8 274.313L964.2 211.813C964.3 177.413 964.3 144.113 964.2 137.813L963.8 126.413L948.7 111.113L933.6 95.8127H841.1H748.5L734.4 110.113ZM870.8 188.813C871 201.213 870.8 218.613 870.5 227.513L869.8 243.813H838.9H808V204.813V165.813L839.3 166.013L870.5 166.313L870.8 188.813Z" fill="currentColor"/>
    <path d="M996.7 113.613L978.8 131.513L978.2 202.713L977.7 274.013L996.1 292.413L1014.5 310.813H1106.5H1198.5L1213.8 295.113L1229 279.313V263.513V247.613L1215.1 233.513L1201.2 219.413L1170.4 219.213L1139.6 218.913L1137.8 220.513L1136 222.113V233.513V244.813L1104.8 244.613L1073.5 244.313V205.313V166.313L1104.8 166.013L1136 165.813V177.113V188.413L1137.2 189.613L1138.4 190.813H1168.9H1199.5L1214.4 175.713L1229.3 160.613L1229.7 143.113L1230.1 125.513L1215.6 110.913L1201.1 96.3127L1107.8 96.0127L1014.6 95.8127L996.7 113.613Z" fill="currentColor"/>
    <path d="M1275.9 100.213C1272.9 102.713 1264.9 110.413 1258 117.313L1245.5 130.013L1244.8 137.613C1244.5 141.813 1244.1 175.013 1244.1 211.213L1244 277.113L1260.3 293.813L1276.5 310.513L1279.5 311.213C1281.2 311.613 1322.2 311.713 1370.8 311.613L1459 311.313L1475.8 294.313L1492.5 277.313L1492.8 202.913L1493.1 128.513L1477.1 112.413L1461.1 96.3126L1371.1 96.0126L1281.2 95.8126L1275.9 100.213ZM1400 205.313V244.813H1368H1336V205.313V165.813H1368H1400V205.313Z" fill="currentColor"/>
    <path d="M239.6 114.413L238 115.913V168.613L237.9 221.313L233 226.813L228.1 232.313L228 228.313V224.313L216.5 212.813L205 201.413L204.8 177.313L204.5 153.313L165.6 153.013L126.6 152.813L125.8 154.013C125.4 154.713 125 165.813 125 178.713V202.113L114.5 212.913L104 223.813V226.613L103.9 229.313L100.5 225.313L97 221.313V169.113C97 140.413 96.7 116.313 96.4 115.413L95.8 113.813H61.1H26.5L13.2 127.113L0 140.313V205.313V270.213L20.3 290.513L40.6 310.813L81.5 310.613L122.5 310.313L132.7 296.813C148.5 276.113 164.4 255.913 165 255.913C165.3 255.913 174.1 267.413 184.5 281.513C195 295.613 204.3 307.913 205.3 309.013L207 310.813H247.4H287.8L307.8 290.313L327.7 269.813L328.2 257.513C328.5 250.813 328.8 221.413 328.9 192.113L329.1 138.913L317.4 126.613C310.9 119.813 305.1 114.013 304.6 113.613C304 113.213 289.5 112.813 272.3 112.813H241.1L239.6 114.413Z" fill="currentColor"/>
  </svg>;
}
const wordmarkStyle = stylex.create({
  // em-box sizing: width follows fontSize (the mark is 2008/395 ~= 5.08:1).
  mark: {display: 'block', width: '5.0823em', height: '1em', color: colors.foreground, flexShrink: 0},
});

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
