export function pageHead(title: string, description: string, path: string) {
  const name = `${title} — whipcode`
  const canonical = __DOCS_SITE_URL__ ? `${__DOCS_SITE_URL__}${path}` : undefined
  return {
    meta: [
      { title: name }, { name: 'description', content: description },
      { property: 'og:title', content: name }, { property: 'og:description', content: description },
      { property: 'og:type', content: 'website' },
      ...(canonical && path !== '/404' ? [{ property: 'og:url', content: canonical }, { property: 'og:image', content: `${__DOCS_SITE_URL__}/social-card.svg` }] : []),
      { name: 'robots', content: canonical && path !== '/404' ? 'index,follow' : 'noindex,nofollow' },
    ],
    links: canonical && path !== '/404' ? [{ rel: 'canonical', href: canonical }] : [],
  }
}
