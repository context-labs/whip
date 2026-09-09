import * as stylex from '@stylexjs/stylex';
import type { StyleXStyles } from '@stylexjs/stylex';
import { Button as BaseButton } from '@base-ui/react/button';
import { Tooltip as BaseTooltip } from '@base-ui/react/tooltip';
import { ToggleGroup as BaseToggleGroup } from '@base-ui/react/toggle-group';
import { Toggle as BaseToggle } from '@base-ui/react/toggle';
import { mergeProps } from '@base-ui/react/merge-props';
import { LoaderCircle, Copy, Check } from 'lucide-react';
import { useState } from 'react';
import type { ComponentPropsWithRef, ReactElement, ReactNode } from 'react';
import { styles } from './styles.stylex';

export type Styled = {xstyle?: StyleXStyles};
export type ButtonProps = ComponentPropsWithRef<'button'> & Styled & {variant?: 'primary' | 'secondary' | 'ghost' | 'danger'; size?: 'sm' | 'md' | 'lg'; loading?: boolean};
export function Button({variant = 'secondary', size = 'md', loading, xstyle, children, disabled, ...props}: ButtonProps) {
  return <BaseButton {...mergeProps(stylex.props(styles.control, styles.button, styles[variant], size === 'sm' && styles.small, size === 'lg' && styles.large, xstyle), props)} type={props.type ?? 'button'} disabled={disabled || loading} aria-busy={loading || undefined}>{loading && <Spinner />}{children}</BaseButton>;
}
export function IconButton({label, children, xstyle, ...props}: ButtonProps & {label: string}) {
  return <Tooltip label={label}><Button {...props} aria-label={label} xstyle={[styles.icon, xstyle]}>{children}</Button></Tooltip>;
}
type TooltipProps = {label: ReactNode; children: ReactElement; delay?: number} & Styled &
  Pick<BaseTooltip.Root.Props, 'disableHoverablePopup'> &
  Pick<BaseTooltip.Positioner.Props, 'side' | 'align' | 'sideOffset' | 'collisionPadding' | 'collisionAvoidance'>;
export function Tooltip({label, children, delay, disableHoverablePopup, xstyle, ...position}: TooltipProps) {
  return <BaseTooltip.Root disableHoverablePopup={disableHoverablePopup}><BaseTooltip.Trigger render={children} delay={delay}/><BaseTooltip.Portal><BaseTooltip.Positioner sideOffset={7} {...position} {...stylex.props(styles.positioner)}><BaseTooltip.Popup {...stylex.props(styles.tooltip, xstyle)}>{label}</BaseTooltip.Popup></BaseTooltip.Positioner></BaseTooltip.Portal></BaseTooltip.Root>;
}
const spin = stylex.keyframes({from: {transform: 'rotate(0deg)'}, to: {transform: 'rotate(360deg)'}});
const spinnerStyles = stylex.create({spin: {animationName: spin, animationDuration: '1s', animationTimingFunction: 'linear', animationIterationCount: 'infinite'}});
export function Spinner({label = 'Loading', size = 14}: {label?: string; size?: number}) {
  return <LoaderCircle {...stylex.props(spinnerStyles.spin)} width={size} height={size} aria-label={label} role="img" />;
}
export function ButtonGroup({xstyle, ...props}: ComponentPropsWithRef<'div'> & Styled) { return <div role="group" {...mergeProps(stylex.props(styles.inline, xstyle), props)}/>; }
export function ToggleGroup({value, onValueChange, items, label}: {value: string[]; onValueChange: (value: string[]) => void; items: {value: string; label: ReactNode}[]; label: string}) {
  return <BaseToggleGroup value={value} onValueChange={onValueChange} aria-label={label} {...stylex.props(styles.inline)}>{items.map(item => <BaseToggle key={item.value} value={item.value} className={state => stylex.props(styles.control, styles.button, state.pressed && styles.tabActive).className}>{item.label}</BaseToggle>)}</BaseToggleGroup>;
}
export function Link({xstyle, ...props}: ComponentPropsWithRef<'a'> & Styled) {return <a {...mergeProps(stylex.props(styles.link, xstyle), props)}/>;}
export function Kbd({children}: {children: ReactNode}) {return <kbd {...stylex.props(styles.kbd)}>{children}</kbd>;}
export function CopyButton({text, label = 'Copy', copy, onError, xstyle}: Styled & {text: string; label?: string; copy?: (text: string) => Promise<void>; onError?: (error: unknown) => void}) {
  const [copied, setCopied] = useState<string | null>(null);
  const [error, setError] = useState(false);
  return <IconButton xstyle={xstyle} label={error ? 'Could not copy. Try again.' : copied === text ? 'Copied' : label} variant="ghost" onBlur={() => {setCopied(null); setError(false);}} onClick={() => {void Promise.resolve().then(() => (copy ?? (value => navigator.clipboard.writeText(value)))(text)).then(() => {setCopied(text); setError(false);}, error => {setCopied(null); setError(true); onError?.(error);});}}>{copied === text ? <Check size={14}/> : <Copy size={14}/>}</IconButton>;
}
