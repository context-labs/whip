import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';

// The mark in an MCP server row: a bundled logo for the common vendors, a
// logo the daemon looked up for the rest, or a monogram that is a designed
// placeholder rather than a missing image. The name beside the tile is what
// screen readers announce, so the tile itself is decoration.

type Marks = Record<string, string>;

/** The bundled marks, one lazy chunk keyed by registrable domain (see scripts/mcp-brands.mjs). */
export function useBrandMarks() {
  return useQuery({
    queryKey: ['mcp-brand-marks'],
    queryFn: () => import('./assets/mcp-brands.json').then(module => module.default as Marks),
    staleTime: Infinity, gcTime: Infinity, retry: false,
  }).data;
}

/** Marks the daemon resolves for keys the bundle lacks; nothing is asked when the daemon predates the operation. */
export function useBrandIcons(client: WhipClient, keys: readonly string[]) {
  const runtimeId = client.getSnapshot().info?.runtime_id;
  const sorted = [...new Set(keys)].sort();
  return useQuery({
    queryKey: ['mcp-brand-icons', runtimeId, sorted],
    queryFn: ({ signal }) => client.mcpImport.brandIcons({ keys: sorted }, { signal }),
    enabled: sorted.length > 0 && client.supports('rpc', 'mcp.brand.icons'),
    staleTime: 30 * 60_000, gcTime: 30 * 60_000, retry: false,
  }).data?.icons;
}

const tintNames = ['accent', 'success', 'info', 'link', 'emphasis'] as const;

/** A stable tint per name so a list of unknown servers has variety without noise. */
export function tintIndex(name: string) {
  let hash = 7;
  for (const char of name) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  return hash % tintNames.length;
}

export function MCPBrandIcon({ name, src, quiet = false }: { name: string; src?: string; quiet?: boolean }) {
  const [broken, setBroken] = useState<string>();
  const image = src && src !== broken ? src : undefined;
  return <span aria-hidden {...stylex.props(styles.tile, image ? styles.image : quiet ? styles.quiet : tints[tintNames[tintIndex(name)]!])}>
    {image ? <img src={image} alt="" loading="lazy" draggable={false} onError={() => setBroken(image)} {...stylex.props(styles.mark)} /> : name.slice(0, 1).toUpperCase()}
  </span>;
}

const tint = (token: string) => ({ backgroundColor: `color-mix(in srgb, ${token} 16%, transparent)`, color: token });
const tints = stylex.create({
  accent: tint(colors.accent),
  success: tint(colors.success),
  info: tint(colors.info),
  link: tint(colors.link),
  emphasis: tint(colors.emphasis),
});
const styles = stylex.create({
  tile: {
    display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 22, height: 22, flexShrink: 0,
    borderRadius: scale.radiusSmall, fontSize: typography.size11, fontWeight: 600, lineHeight: 1, overflow: 'hidden',
  },
  quiet: { backgroundColor: colors.element, color: surface.secondaryText },
  image: { backgroundColor: colors.element },
  mark: { width: 16, height: 16, objectFit: 'contain', borderRadius: 2 },
});
