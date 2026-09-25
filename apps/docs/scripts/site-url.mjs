export function siteUrl(value) {
  if (!value) return ''
  const url = new URL(value)
  if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash) {
    throw new Error('DOCS_SITE_URL must be an HTTPS URL with no credentials, query or fragment')
  }
  if (!/^\/(?:[a-z0-9]+(?:-[a-z0-9]+)*(?:\/[a-z0-9]+(?:-[a-z0-9]+)*)*\/?)?$/.test(url.pathname)) throw new Error('DOCS_SITE_URL path must use lowercase kebab-case segments')
  return url.origin + url.pathname.replace(/\/$/, '')
}
