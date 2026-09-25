import { createRootRoute, HeadContent, Outlet, Scripts, Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import * as stylex from '@stylexjs/stylex'
import { SiteHeader } from '~/components/navigation/SiteHeader'
import { ThemeMenu } from '~/components/ui/theme/ThemeMenu'
import { themeInitScript } from '~/components/ui/theme/theme'
import { styles as skipLinkStyles } from '~/styles/skip-link.stylex'
import { styles as siteStyles } from '~/styles/site.stylex'
import stylesheet from '~/styles/index.css?url'

export const Route = createRootRoute({
  head: () => ({
    meta: [{ charSet: 'utf-8' }, { name: 'viewport', content: 'width=device-width, initial-scale=1' }, { name: 'color-scheme', content: 'dark light' }],
    links: [
      { rel: 'stylesheet', href: stylesheet },
      // TanStack Start renders the document via shellComponent, so the StyleX
      // plugin's transformIndexHtml injection never runs; add the dev-only
      // collected CSS link ourselves (extraction replaces it in production).
      ...(import.meta.env.DEV ? [{ rel: 'stylesheet', href: '/virtual:stylex.css' }] : []),
      { rel: 'icon', type: 'image/svg+xml', href: import.meta.env.BASE_URL + 'favicon.svg' },
    ],
    scripts: import.meta.env.DEV ? [{ type: 'module', src: '/@id/virtual:stylex:runtime' }] : [],
  }),
  shellComponent: Document,
  component: Outlet,
  notFoundComponent: NotFound,
})

function Document({ children }: { children: ReactNode }) {
  return <html lang="en" suppressHydrationWarning>
    <head><script dangerouslySetInnerHTML={{ __html: themeInitScript }} /><HeadContent /></head>
    <body>
      <a {...stylex.props(skipLinkStyles.skipLink)} href="#main-content">Skip to content</a>
      <SiteHeader />
      {children}
      <footer {...stylex.props(siteStyles.siteFooter)} className={`site-footer ${stylex.props(siteStyles.siteFooter).className}`}><span>whipcode</span><div {...stylex.props(siteStyles.footerActions)} className={`footer-actions ${stylex.props(siteStyles.footerActions).className}`}><a href="https://github.com/context-labs/whip">View source on GitHub</a><ThemeMenu /></div></footer>
      <Scripts />
    </body>
  </html>
}
export function NotFound() {
  const mainSx = stylex.props(siteStyles.siteMain)
  return <main id="main-content" {...mainSx} className={`site-main prose ${mainSx.className}`}><h1>Page not found</h1><p>The page may have moved, or the address may be incorrect.</p><Link to="/docs">Browse the documentation</Link></main>
}
