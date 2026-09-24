export type DocHeading = { id: string; text: string; level: 2 | 3 }
export type DocMeta = {
  path: string
  title: string
  navTitle?: string
  description: string
  section: string
  order: number
  headings: DocHeading[]
}
export const docSections = [
  { id: 'start', label: 'Get started' },
  { id: 'usage', label: 'Using whipcode' },
  { id: 'reference', label: 'Reference' },
] as const
