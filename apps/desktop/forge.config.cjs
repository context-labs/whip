const path = require('node:path');
const { copyFile, mkdir } = require('node:fs/promises');
const { readFileSync } = require('node:fs');
const { FusesPlugin } = require('@electron-forge/plugin-fuses');
const { FuseV1Options, FuseVersion } = require('@electron/fuses');

const identity = process.env.WHIP_DESKTOP_SIGN_IDENTITY;
const metadata = JSON.parse(readFileSync(path.join(__dirname, '.stage/app/package.json'), 'utf8'));
const release = JSON.parse(readFileSync(path.join(__dirname, '.stage/app/desktop-config.json'), 'utf8'));
const appName = metadata.productName;
const bundleId = release.channel === 'beta' ? 'com.contextlabs.whip.beta' : 'com.contextlabs.whip';
let notarize;
if (process.env.WHIP_DESKTOP_NOTARIZE === '1') {
  if (!identity) throw new Error('Notarization requires a signed build');
  if (process.env.WHIP_DESKTOP_NOTARY_PROFILE) notarize = { keychainProfile: process.env.WHIP_DESKTOP_NOTARY_PROFILE };
  else if (process.env.WHIP_DESKTOP_NOTARY_KEY && process.env.WHIP_DESKTOP_NOTARY_KEY_ID && process.env.WHIP_DESKTOP_NOTARY_ISSUER)
    notarize = { appleApiKey: process.env.WHIP_DESKTOP_NOTARY_KEY, appleApiKeyId: process.env.WHIP_DESKTOP_NOTARY_KEY_ID,
      appleApiIssuer: process.env.WHIP_DESKTOP_NOTARY_ISSUER };
  else throw new Error('Configure a notarization keychain profile or API key');
}
module.exports = {
  outDir: path.join(__dirname, 'out'),
  packagerConfig: {
    name: appName, executableName: appName, appBundleId: bundleId,
    appCategoryType: 'public.app-category.developer-tools', darwinDarkModeSupport: true,
    asar: true, prune: false, extendInfo: { LSMinimumSystemVersion: '14.0',
      CFBundleURLTypes: [{ CFBundleURLName: 'Whip session', CFBundleURLSchemes: [release.channel === 'beta' ? 'whip-beta' : 'whip'] }] },
    afterCopy: [(buildPath, _version, _platform, _arch, callback) => {
      void (async () => {
        const helpers = path.resolve(buildPath, '../../Helpers');
        await mkdir(helpers, { recursive: true });
        for (const name of ['whip', 'whip-computer']) await copyFile(path.join(__dirname, '.stage/native', name), path.join(helpers, name));
      })().then(() => callback(), callback);
    }],
    ...(identity ? { osxSign: { identity, hardenedRuntime: true,
      // Native files were signed before Go embed and manifest hashing. Preserve them.
      ignore: file => /\/Helpers\/whip(?:-computer)?$/.test(file),
      optionsForFile: () => ({ entitlements: path.join(__dirname, 'resources/electron.entitlements.plist') }) } } : {}),
    ...(notarize ? { osxNotarize: notarize } : {}),
  },
  makers: [{ name: '@electron-forge/maker-dmg', platforms: ['darwin'], config: { format: 'ULFO' } },
    { name: '@electron-forge/maker-zip', platforms: ['darwin'],
      config: release.updateURL ? { macUpdateManifestBaseUrl: release.updateURL.replace(/\/RELEASES\.json$/, '') } : {} }],
  plugins: [new FusesPlugin({ version: FuseVersion.V1,
    [FuseV1Options.RunAsNode]: false, [FuseV1Options.EnableNodeOptionsEnvironmentVariable]: false,
    [FuseV1Options.EnableNodeCliInspectArguments]: false,
    [FuseV1Options.EnableEmbeddedAsarIntegrityValidation]: true,
    [FuseV1Options.OnlyLoadAppFromAsar]: true,
    [FuseV1Options.EnableCookieEncryption]: true,
    [FuseV1Options.GrantFileProtocolExtraPrivileges]: false })],
};
