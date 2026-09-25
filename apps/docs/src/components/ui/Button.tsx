import type { ButtonHTMLAttributes } from 'react';
import { Tooltip } from '@base-ui/react/tooltip';
import { Menu } from '@base-ui/react/menu';
import { Icon } from './Icons';

export function Button({ className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button type="button" className={`button ${className}`} {...props} />;
}
export function IconButton({ label, className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { label: string }) {
  return <Tooltip.Provider><Tooltip.Root>
    <Tooltip.Trigger aria-label={label} className={`icon-button ${className}`} {...props} />
    <Tooltip.Portal><Tooltip.Positioner sideOffset={6}><Tooltip.Popup className="tooltip">{label}</Tooltip.Popup></Tooltip.Positioner></Tooltip.Portal>
  </Tooltip.Root></Tooltip.Provider>;
}
export type SplitButtonAction = { label: string; onClick: () => void; disabled?: boolean };
export function SplitButton({ label, onClick, actions, disabled }: {
  label: string; onClick: () => void; actions: readonly SplitButtonAction[]; disabled?: boolean;
}) {
  return <div className="split-button">
    <Button onClick={onClick} disabled={disabled}>{label}</Button>
    <Menu.Root><Menu.Trigger className="button split-trigger" aria-label={`More actions for ${label}`} disabled={disabled}><Icon name="chevron" /></Menu.Trigger>
      <Menu.Portal><Menu.Positioner sideOffset={6} align="end"><Menu.Popup className="menu-popup">
        {actions.map(action => <Menu.Item key={action.label} className="menu-item" onClick={action.onClick} disabled={action.disabled}>{action.label}</Menu.Item>)}
      </Menu.Popup></Menu.Positioner></Menu.Portal>
    </Menu.Root>
  </div>;
}
