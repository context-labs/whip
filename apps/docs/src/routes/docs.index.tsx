import { createFileRoute, redirect } from '@tanstack/react-router'
import { docRedirects } from '~/features/docs/content/redirects'

export const Route = createFileRoute('/docs/')({
  beforeLoad: ({ location }) => { throw redirect({ href: docRedirects['/docs'] + location.searchStr + location.hash, replace: true, statusCode: 308 }) },
})
