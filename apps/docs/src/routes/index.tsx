import { createFileRoute, redirect } from '@tanstack/react-router'
import { sitePath } from '~/features/docs/content/site-path'
import { docRedirects } from '~/features/docs/content/redirects'

export const Route = createFileRoute('/')({
  beforeLoad: ({ location }) => { throw redirect({ href: sitePath(docRedirects['/']) + location.searchStr + location.hash, replace: true, statusCode: 308 }) },
})
