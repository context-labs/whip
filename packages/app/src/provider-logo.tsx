import { Sparkles } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors } from '@whip/ui/tokens.stylex';

const logos = new URL('./assets/providers.svg', import.meta.url).href;
const logoIDs: Record<string, string> = {
  'inference-net': 'inference', inference: 'inference', 'inference.net': 'inference',
  openrouter: 'openrouter', openai: 'openai', 'openai-codex': 'openai',
  anthropic: 'anthropic', google: 'google', gemini: 'google', 'google-generative-ai': 'google',
  deepseek: 'deepseek', mistral: 'mistral', xai: 'xai', 'x-ai': 'xai',
};

/** Brand the routing provider, independently of the model's publisher. */
export function ProviderLogo({ id, size = 16 }: { id: string; size?: number }) {
  const key = id.toLowerCase();
  const name = Object.hasOwn(logoIDs, key) ? logoIDs[key] : undefined;
  return name ? <svg width={size} height={size} viewBox="0 0 40 40" aria-hidden="true" {...stylex.props(styles.logo)}>
    <use href={`${logos}#${name}`} />
  </svg> : <Sparkles size={size} aria-hidden="true" {...stylex.props(styles.logo)} />;
}

const styles = stylex.create({ logo: { flexShrink: 0, color: colors.foreground } });
