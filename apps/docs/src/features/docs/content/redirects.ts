export const docRedirects: Readonly<Record<string, string>> = {
  '/': '/docs/quickstart',
  '/docs': '/docs/quickstart',
  '/docs/introduction': '/docs/quickstart',
  '/docs/getting-started': '/docs/quickstart',
  '/docs/installation': '/docs/download',
  '/docs/using-whipcode/cli': '/docs/tui',
  '/docs/tools-and-permissions': '/docs/permissions',
}

export function docRedirect(pathname: string) {
  const clean = pathname.replace(/(?:\/index\.html|\.html|\/+)$/, '') || '/'
  return docRedirects[clean]
}
