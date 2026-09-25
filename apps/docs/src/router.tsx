import { createRouter } from '@tanstack/react-router'
import { routeTree } from './routeTree.gen'
export function getRouter() {
  return createRouter({ routeTree, scrollRestoration: true, trailingSlash: 'never', defaultPreload: 'intent' })
}
declare module '@tanstack/react-router' {
  interface Register { router: ReturnType<typeof getRouter> }
}
