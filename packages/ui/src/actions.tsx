import * as stylex from '@stylexjs/stylex';
import type { StyleXStyles } from '@stylexjs/stylex';
import { Button as BaseButton } from '@base-ui/react/button';
import { Tooltip as BaseTooltip } from '@base-ui/react/tooltip';
import { ToggleGroup as BaseToggleGroup } from '@base-ui/react/toggle-group';
import { Toggle as BaseToggle } from '@base-ui/react/toggle';
import { mergeProps } from '@base-ui/react/merge-props';
import { LoaderCircle, Copy, Check } from 'lucide-react';
import { useContext, useLayoutEffect, useRef, useState } from 'react';
import { ClipboardContext } from './clipboard';
import type { ComponentPropsWithRef, ReactElement, ReactNode } from 'react';
import { styles } from './styles.stylex';
import { scale } from './tokens.stylex';
import { useNativeOverlay } from './native-surfaces';

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
  const overlay = useNativeOverlay();
  return <BaseTooltip.Root {...overlay} disableHoverablePopup={disableHoverablePopup}><BaseTooltip.Trigger render={children} delay={delay}/><BaseTooltip.Portal><BaseTooltip.Positioner sideOffset={7} {...position} {...stylex.props(styles.positioner)}><BaseTooltip.Popup {...stylex.props(styles.tooltip, xstyle)}>{label}</BaseTooltip.Popup></BaseTooltip.Positioner></BaseTooltip.Portal></BaseTooltip.Root>;
}
const spin = stylex.keyframes({from: {transform: 'rotate(0deg)'}, to: {transform: 'rotate(360deg)'}});
const spinnerStyles = stylex.create({spin: {animationName: {default: spin, [scale.reducedMotion]: 'none'}, animationDuration: '1s', animationTimingFunction: 'linear', animationIterationCount: 'infinite'}});
export function Spinner({label = 'Loading', size = 14}: {label?: string; size?: number}) {
  return <LoaderCircle {...stylex.props(spinnerStyles.spin)} width={size} height={size} aria-label={label} role="img" />;
}
export function ButtonGroup({xstyle, ...props}: ComponentPropsWithRef<'div'> & Styled) { return <div role="group" {...mergeProps(stylex.props(styles.inline, xstyle), props)}/>; }
/** Quiet toolbar choices: filled when selected, ghost when inactive. */
export function ToggleGroup({value, onValueChange, items, label, size = 'md', multiple = false, xstyle}: Styled & {value: string[]; onValueChange: (value: string[]) => void; items: {value: string; label: ReactNode; disabled?: boolean}[]; label: string; size?: 'sm' | 'md'; multiple?: boolean}) {
  return <BaseToggleGroup value={value} onValueChange={onValueChange} multiple={multiple} aria-label={label} {...stylex.props(styles.inline, styles.toggleGroup, xstyle)}>{items.map(item => <BaseToggle key={item.value} value={item.value} disabled={item.disabled} className={state => stylex.props(styles.control, styles.button, state.pressed ? styles.secondary : styles.ghost, size === 'sm' && styles.small).className}>{item.label}</BaseToggle>)}</BaseToggleGroup>;
}
export function Link({xstyle, ...props}: ComponentPropsWithRef<'a'> & Styled) {return <a {...mergeProps(stylex.props(styles.link, xstyle), props)}/>;}
export function Kbd({children, xstyle}: {children: ReactNode} & Styled) {return <kbd {...stylex.props(styles.kbd, xstyle)}>{children}</kbd>;}
const copyStyles = stylex.create({
  feedback: {display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 4, minWidth: 0},
  error: {maxWidth: 240, overflowWrap: 'anywhere', fontSize: 'inherit'},
});
export function CopyButton({text, label = 'Copy', showLabel = false, showError = false, copy, onError, xstyle}: Styled & {text: string; label?: string; showLabel?: boolean; showError?: boolean; copy?: (text: string) => Promise<void>; onError?: (error: unknown) => void}) {
  const platformCopy = useContext(ClipboardContext);
  const [feedback, setFeedback] = useState<{text: string; error?: string} | null>(null);
  const request = useRef(0);
  useLayoutEffect(() => {
    setFeedback(null);
    // Retire pending feedback when streaming changes the text or the button unmounts.
    return () => {request.current++;};
  }, [text]);
  const current = feedback?.text === text ? feedback : null;
  const copied = current !== null && !current.error;
  const currentLabel = current?.error ? 'Could not copy. Try again.' : copied ? 'Copied' : label;
  const icon = copied ? <Check size={14}/> : <Copy size={14}/>;
  const props: ButtonProps = {
    xstyle, variant: 'ghost',
    onBlur: () => setFeedback(previous => showError && previous?.error ? previous : null),
    onClick: () => {
      const id = ++request.current;
      void Promise.resolve().then(() => (copy ?? platformCopy)(text)).then(() => {
        if (id === request.current) setFeedback({text});
      }, error => {
        if (id !== request.current) return;
        setFeedback({text, error: error instanceof Error && error.message ? error.message : 'Select the text to copy it manually.'});
        onError?.(error);
      });
    },
  };
  const button = showLabel ? <Button {...props}>{icon}{currentLabel}</Button> : <IconButton {...props} label={currentLabel}>{icon}</IconButton>;
  return showError ? <div {...stylex.props(copyStyles.feedback)}>{button}{current?.error && <span role="alert" {...stylex.props(copyStyles.error)}>Could not copy. {current.error} Try again using the copy button.</span>}</div> : button;
}
