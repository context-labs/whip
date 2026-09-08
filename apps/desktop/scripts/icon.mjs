import { execFile } from 'node:child_process';
import { mkdir, mkdtemp, rm } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';

// Convert the existing Whip brand asset to the native macOS icon container.
const exec = promisify(execFile);
const temporary = await mkdtemp('/tmp/whip-icon-');
try {
  const directory = path.join(temporary, 'Whip.iconset');
  await mkdir(directory);
  for (const size of [16, 32, 128, 256, 512]) for (const scale of [1, 2]) {
    const pixels = String(size * scale);
    await exec('/usr/bin/sips', ['-z', pixels, pixels, path.join(repositoryRoot, 'apps/mobile/assets/icon.png'), '--out',
      path.join(directory, `icon_${size}x${size}${scale === 2 ? '@2x' : ''}.png`)]);
  }
  await exec('/usr/bin/iconutil', ['-c', 'icns', directory, '-o', path.join(repositoryRoot, 'apps/desktop/resources/Whip.icns')]);
} finally { await rm(temporary, { recursive: true, force: true }); }
