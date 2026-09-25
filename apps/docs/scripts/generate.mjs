import { Generator, getConfig } from '@tanstack/router-generator'
import { appRoot, generateManifest } from './content.mjs'
await generateManifest()
await new Generator({ config: getConfig({ target: 'react', routesDirectory: './src/routes', generatedRouteTree: './src/routeTree.gen.ts' }, appRoot), root: appRoot }).run()
