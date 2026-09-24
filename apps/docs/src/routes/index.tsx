import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/')({
  beforeLoad: () => {
    throw redirect({ to: '/docs/$', params: { _splat: 'getting-started' }, replace: true, statusCode: 308 })
  },
})
