import { createFileRoute } from '@tanstack/react-router'
import { NotFound } from './__root'
import { pageHead } from '~/features/docs/content/head'
export const Route = createFileRoute('/404')({
  head: () => pageHead('Page not found', 'This page could not be found. Browse the whipcode documentation.', '/404'),
  component: NotFound,
})
