import { Menu } from '@base-ui/react/menu';
import { CopyButton } from '../../../components/ui/CopyButton';
import { Icon } from '../../../components/ui/Icons';
import { useHydrated } from '../../../components/ui/useHydrated';

export function DocPageActions({ source, filename }: { source: string; filename: string }) {
  const hydrated = useHydrated();
  if (!hydrated) return null;
  return <div className="doc-page-actions">
    <CopyButton text={source} label="Copy page" failureMessage="Copy failed. Use the menu to download the page source." />
    <Menu.Root>
      <Menu.Trigger className="doc-page-actions-trigger" aria-label="More page actions"><Icon name="chevron" strokeWidth={2} /></Menu.Trigger>
      <Menu.Portal><Menu.Positioner sideOffset={6} align="end"><Menu.Popup className="menu-popup">
        <Menu.Item className="menu-item" render={<a href={`data:text/plain;charset=utf-8,${encodeURIComponent(source)}`} download={filename} />}>Download page source</Menu.Item>
      </Menu.Popup></Menu.Positioner></Menu.Portal>
    </Menu.Root>
  </div>;
}
