import * as stylex from '@stylexjs/stylex';
import { DiscordMark, Icon } from '../ui/Icons';
import { CommunityLink } from './NavLinks';
import { sitePath } from '../../features/docs/content/site-path';
import { Wordmark } from './Wordmark';
import { styles as headerStyles } from './SiteHeader.stylex';
import { styles as buttonStyles } from '../ui/Button.stylex';

export function SiteHeader() {
  const headerSx = stylex.props(headerStyles.siteHeader);
  const innerSx = stylex.props(headerStyles.siteHeaderInner);
  const brandSx = stylex.props(headerStyles.brandLink);
  const actionsSx = stylex.props(headerStyles.headerActions);
  const communitySx = stylex.props(headerStyles.headerCommunity);
  const downloadSx = stylex.props(buttonStyles.button, headerStyles.headerDownload);
  const labelSx = stylex.props(headerStyles.headerDownloadLabel);
  return <header {...headerSx} className={`site-header ${headerSx.className}`}>
    <div {...innerSx} className={`site-header-inner ${innerSx.className}`}>
      <a href={sitePath('/')} {...brandSx} className={`brand-link ${brandSx.className}`} aria-label="whipcode home"><Wordmark /></a>
      <div {...actionsSx} className={`header-actions ${actionsSx.className}`}>
        <nav {...communitySx} className={`header-community ${communitySx.className}`} aria-label="Community">
          <CommunityLink href="https://discord.gg/K2deYSXNu" label="Discord"><DiscordMark /></CommunityLink>
          <CommunityLink />
        </nav>
        <a {...downloadSx} className={`header-download ${downloadSx.className}`} href={sitePath('/docs/download')} aria-label="Download">
          <Icon name="download" strokeWidth={2} />
          <span {...labelSx} className={`header-download-label ${labelSx.className}`}>Download</span>
        </a>
      </div>
    </div>
  </header>;
}
