export function sitePath(href: string) {
  return href.startsWith('/') && !href.startsWith('//')
    ? import.meta.env.BASE_URL.replace(/\/$/, '') + href
    : href
}
