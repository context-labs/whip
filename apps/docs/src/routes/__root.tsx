import { createRootRoute, HeadContent, Outlet, Scripts, Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { SiteHeader } from '~/components/navigation/SiteHeader'
import { ThemeMenu } from '~/components/ui/theme/ThemeMenu'
import { themeInitScript } from '~/components/ui/theme/theme'
import stylesheet from '~/styles/index.css?url'

export const Route = createRootRoute({
  head: () => ({
    meta: [{ charSet: 'utf-8' }, { name: 'viewport', content: 'width=device-width, initial-scale=1' }, { name: 'color-scheme', content: 'dark light' }],
    links: [{ rel: 'stylesheet', href: stylesheet }, { rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' }],
  }),
  shellComponent: Document,
  component: Outlet,
  notFoundComponent: NotFound,
})

function Document({ children }: { children: ReactNode }) {
  return <html lang="en" suppressHydrationWarning>
    <head><script dangerouslySetInnerHTML={{ __html: themeInitScript }} /><HeadContent /></head>
    <body>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <SiteHeader />
      {children}
      <footer className="site-footer"><span>whipcode</span><div className="footer-actions"><a href="https://github.com/context-labs/whip">View source on GitHub</a><ThemeMenu /></div></footer>
      <Scripts />
    </body>
  </html>
}
export function NotFound() {
  return <main id="main-content" className="site-main prose"><h1>Page not found</h1><p>The page may have moved, or the address may be incorrect.</p><Link to="/docs">Browse the documentation</Link></main>
}
