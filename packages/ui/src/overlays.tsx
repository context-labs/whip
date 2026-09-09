import * as stylex from '@stylexjs/stylex';
import { Dialog as BaseDialog } from '@base-ui/react/dialog';
import { AlertDialog as BaseAlertDialog } from '@base-ui/react/alert-dialog';
import { Menu as BaseMenu } from '@base-ui/react/menu';
import { ContextMenu as BaseContextMenu } from '@base-ui/react/context-menu';
import { Popover as BasePopover } from '@base-ui/react/popover';
import { ChevronRight, X } from 'lucide-react';
import { useRef } from 'react';
import type { ReactElement, ReactNode } from 'react';
import { styles } from './styles.stylex';
import { Button, Kbd } from './actions';
import type { Styled } from './actions';
import { Combobox } from './forms';
import type { Option } from './forms';

export interface DialogProps extends Styled {open: boolean; onOpenChange: (open: boolean) => void; title: ReactNode; header?: ReactNode; description?: ReactNode; children?: ReactNode; footer?: ReactNode; closeLabel?: string; initialFocus?: BaseDialog.Popup.Props['initialFocus']; finalFocus?: BaseDialog.Popup.Props['finalFocus']}
export function Dialog({open, onOpenChange, title, header, description, children, footer, closeLabel = 'Close', initialFocus, finalFocus, xstyle}: DialogProps) {
  return <BaseDialog.Root open={open} onOpenChange={onOpenChange}><BaseDialog.Portal><BaseDialog.Backdrop {...stylex.props(styles.backdrop)}/><BaseDialog.Popup initialFocus={initialFocus} finalFocus={finalFocus} {...stylex.props(styles.dialog, xstyle)}><div {...stylex.props(styles.dialogHeader)}><div {...stylex.props(header != null && styles.grow)}><BaseDialog.Title {...stylex.props(styles.dialogTitle, header != null && dialogStyles.hiddenTitle)}>{title}</BaseDialog.Title>{header}{description && <BaseDialog.Description {...stylex.props(styles.dialogDescription)}>{description}</BaseDialog.Description>}</div><BaseDialog.Close render={<Button aria-label={closeLabel} variant="ghost" xstyle={styles.icon}><X size={16}/></Button>}/></div><div {...stylex.props(styles.stack)}>{children}</div>{footer && <div {...stylex.props(styles.footer)}>{footer}</div>}</BaseDialog.Popup></BaseDialog.Portal></BaseDialog.Root>;
}
export function Sheet({xstyle, ...props}: DialogProps) {return <Dialog {...props} xstyle={[styles.sheet, xstyle]}/>;}
export function AlertDialog({open, onOpenChange, title, description, children, confirmLabel = 'Continue', onConfirm, loading, danger = false, finalFocus, xstyle}: Omit<DialogProps, 'footer' | 'closeLabel' | 'initialFocus'> & {confirmLabel?: string; onConfirm: () => void; loading?: boolean; danger?: boolean}) {
  return <BaseAlertDialog.Root open={open} onOpenChange={onOpenChange}><BaseAlertDialog.Portal><BaseAlertDialog.Backdrop {...stylex.props(styles.backdrop)}/><BaseAlertDialog.Popup finalFocus={finalFocus} {...stylex.props(styles.dialog, xstyle)}><BaseAlertDialog.Title {...stylex.props(styles.dialogTitle)}>{title}</BaseAlertDialog.Title>{description && <BaseAlertDialog.Description {...stylex.props(styles.dialogDescription)}>{description}</BaseAlertDialog.Description>}{children}<div {...stylex.props(styles.footer)}><BaseAlertDialog.Close render={<Button disabled={loading}>Cancel</Button>}/><Button variant={danger ? 'danger' : 'primary'} loading={loading} onClick={onConfirm}>{confirmLabel}</Button></div></BaseAlertDialog.Popup></BaseAlertDialog.Portal></BaseAlertDialog.Root>;
}
export interface MenuItem {id: string; label: ReactNode; onSelect?: () => void; disabled?: boolean; danger?: boolean; shortcut?: string; icon?: ReactNode; separator?: boolean; items?: readonly MenuItem[]}
function MenuItems({items}: {items: readonly MenuItem[]}) {
  return items.map(item => {
    if (item.separator) return <BaseMenu.Separator key={item.id} {...stylex.props(styles.separator)}/>;
    const content = <>{item.icon}<span {...stylex.props(styles.grow)}>{item.label}</span>{item.shortcut && <Kbd>{item.shortcut}</Kbd>}</>;
    if (item.items) return <BaseMenu.SubmenuRoot key={item.id}>
      <BaseMenu.SubmenuTrigger disabled={item.disabled} className={state => stylex.props(styles.item, state.highlighted && styles.highlighted, state.disabled && styles.disabled).className}>{content}<ChevronRight size={14} aria-hidden="true"/></BaseMenu.SubmenuTrigger>
      <BaseMenu.Portal><BaseMenu.Positioner sideOffset={4} {...stylex.props(styles.positioner)}><BaseMenu.Popup {...stylex.props(styles.popup)}><MenuItems items={item.items}/></BaseMenu.Popup></BaseMenu.Positioner></BaseMenu.Portal>
    </BaseMenu.SubmenuRoot>;
    return <BaseMenu.Item key={item.id} disabled={item.disabled} onClick={item.onSelect} className={state => stylex.props(styles.item, state.highlighted && styles.highlighted, state.disabled && styles.disabled, item.danger && styles.danger).className}>{content}</BaseMenu.Item>;
  });
}
export function Menu({trigger, items, align = 'end', onOpenChange}: {trigger: ReactElement; items: readonly MenuItem[]; align?: 'start' | 'end'; onOpenChange?: (open: boolean) => void}) {
  return <BaseMenu.Root onOpenChange={onOpenChange}><BaseMenu.Trigger render={trigger}/><BaseMenu.Portal><BaseMenu.Positioner align={align} sideOffset={6} {...stylex.props(styles.positioner)}><BaseMenu.Popup {...stylex.props(styles.popup)}><MenuItems items={items}/></BaseMenu.Popup></BaseMenu.Positioner></BaseMenu.Portal></BaseMenu.Root>;
}
export function ContextMenu({children, items, onOpenChange}: {children: ReactElement; items: readonly MenuItem[]; onOpenChange?: (open: boolean) => void}) {
  return <BaseContextMenu.Root onOpenChange={onOpenChange}><BaseContextMenu.Trigger render={children}/><BaseContextMenu.Portal><BaseContextMenu.Positioner {...stylex.props(styles.positioner)}><BaseContextMenu.Popup {...stylex.props(styles.popup)}><MenuItems items={items}/></BaseContextMenu.Popup></BaseContextMenu.Positioner></BaseContextMenu.Portal></BaseContextMenu.Root>;
}
export function Popover({trigger, title, children, open, onOpenChange, xstyle}: {trigger: ReactElement; title?: ReactNode; children: ReactNode; open?: boolean; onOpenChange?: (value: boolean) => void} & Styled) {
  return <BasePopover.Root open={open} onOpenChange={onOpenChange}><BasePopover.Trigger render={trigger}/><BasePopover.Portal><BasePopover.Positioner sideOffset={8} {...stylex.props(styles.positioner)}><BasePopover.Popup {...stylex.props(styles.popup, xstyle)}>{title && <BasePopover.Title {...stylex.props(styles.label)}>{title}</BasePopover.Title>}{children}</BasePopover.Popup></BasePopover.Positioner></BasePopover.Portal></BasePopover.Root>;
}
export function CommandPicker({open, onOpenChange, items, onSelect}: {open: boolean; onOpenChange: (open: boolean) => void; items: readonly Option[]; onSelect: (value: string) => void}) {
  const input = useRef<HTMLInputElement>(null);
  return <Dialog open={open} onOpenChange={onOpenChange} title="Commands" initialFocus={input}><Combobox ref={input} options={items} label="Search commands" placeholder="Find an action…" value={null} onValueChange={value => {onOpenChange(false); onSelect(value);}}/></Dialog>;
}

const dialogStyles = stylex.create({ hiddenTitle: { position: 'absolute', width: 1, height: 1, overflow: 'hidden', clipPath: 'inset(50%)', whiteSpace: 'nowrap' } });
