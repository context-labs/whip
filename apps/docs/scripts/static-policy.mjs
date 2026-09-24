export function robotsText(origin) {
  const lines = origin ? ['User-agent: *', 'Allow: /', `Sitemap: ${origin}/sitemap.xml`, ''] : ['User-agent: *', 'Disallow: /', '']
  return lines.join(String.fromCharCode(10))
}

// Reject grammar implementations, not a harmless mention of Prism in article copy.
export const runtimeHighlighter = /refractor[.]highlight|Prism[.]languages|function bash[(]Prism[)]|function javascript[(]Prism[)]/
