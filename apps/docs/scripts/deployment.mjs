import { siteUrl } from './site-url.mjs'

export const environments = {
  production: { branch: 'main', origin: 'https://inference.net/whipcode', worker: 'whipcode-docs', indexable: true },
  preview: { branch: 'development', origin: 'https://inference.cool/whipcode', worker: 'whipcode-docs-preview', indexable: false },
}

export function docsSite(env = process.env) {
  const target = env.DOCS_ENVIRONMENT
  const origin = siteUrl(env.DOCS_SITE_URL)
  if (!target) return { origin, indexable: false }
  if (!Object.hasOwn(environments, target)) throw new Error('DOCS_ENVIRONMENT must be production or preview')
  const site = environments[target]
  if (origin && origin !== site.origin) throw new Error('DOCS_SITE_URL conflicts with DOCS_ENVIRONMENT')
  return site
}
