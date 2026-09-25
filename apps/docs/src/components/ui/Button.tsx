import type { ButtonHTMLAttributes } from 'react';
import { Tooltip } from '@base-ui/react/tooltip';
import { Menu } from '@base-ui/react/menu';
import * as stylex from '@stylexjs/stylex';
import type { StyleXStyles } from '@stylexjs/stylex';
import { Icon } from './Icons';
import { styles } from './Button.stylex';

export function Button({ className = '', xstyle, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { xstyle?: StyleXStyles }) {
  const stylexProps = stylex.props(styles.button, props.disabled && styles.buttonDisabled, xstyle);
  return <button type="button" {...stylexProps} className={[stylexProps.className, className].filter(Boolean).join(' ') || undefined} {...props} />;
}
export function IconButton({ label, className = '', xstyle, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { label: string; xstyle?: StyleXStyles }) {
  const stylexProps = stylex.props(styles.iconButton, xstyle);
  return <Tooltip.Provider><Tooltip.Root>
    <Tooltip.Trigger aria-label={label} {...stylexProps} className={[stylexProps.className, className].filter(Boolean).join(' ') || undefined} {...props} />
    <Tooltip.Portal><Tooltip.Positioner sideOffset={6}><Tooltip.Popup {...stylex.props(styles.tooltip)}>{label}</Tooltip.Popup></Tooltip.Positioner></Tooltip.Portal>
  </Tooltip.Root></Tooltip.Provider>;
}
export type SplitButtonAction = { label: string; onClick: () => void; disabled?: boolean };
export function SplitButton({ label, onClick, actions, disabled }: {
  label: string; onClick: () => void; actions: readonly SplitButtonAction[]; disabled?: boolean;
}) {
  return <div {...stylex.props(styles.splitButton)}>
    <Button xstyle={styles.splitButtonChild} onClick={onClick} disabled={disabled}>{label}</Button>
    <Menu.Root><Menu.Trigger {...stylex.props(styles.button, styles.splitTrigger, disabled && styles.buttonDisabled)} aria-label={`More actions for ${label}`} disabled={disabled}><Icon name="chevron" /></Menu.Trigger>
      <Menu.Portal><Menu.Positioner sideOffset={6} align="end"><Menu.Popup {...stylex.props(styles.menuPopup)}>
        {actions.map(action => <Menu.Item key={action.label} className={state => stylex.props(styles.menuItem, state.highlighted && styles.menuItemHighlighted, state.disabled && styles.menuItemDisabled).className} onClick={action.onClick} disabled={action.disabled}>{action.label}</Menu.Item>)}
      </Menu.Popup></Menu.Positioner></Menu.Portal>
    </Menu.Root>
  </div>;
}
