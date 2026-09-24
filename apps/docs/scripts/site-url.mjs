export function siteUrl(value) {
  if (!value) return ''
  const url = new URL(value)
  if (url.protocol !== 'https:' || url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
    throw new Error('DOCS_SITE_URL must be an HTTPS origin with no path, credentials, query or fragment')
  }
  return url.origin
}
