import { DiscordMark, Icon } from '../ui/Icons';
import { CommunityLink } from './NavLinks';
import { Wordmark } from './Wordmark';

export function SiteHeader() {
  return <header className="site-header">
    <div className="site-header-inner">
      <a href="/" className="brand-link" aria-label="whipcode home"><Wordmark /></a>
      <div className="header-actions">
        <nav className="header-community" aria-label="Community">
          <CommunityLink href="https://discord.gg/K2deYSXNu" label="Discord"><DiscordMark /></CommunityLink>
          <CommunityLink />
        </nav>
        <a className="button header-download" href="/docs/download" aria-label="Download">
          <Icon name="download" strokeWidth={2} />
          <span className="header-download-label">Download</span>
        </a>
      </div>
    </div>
  </header>;
}
