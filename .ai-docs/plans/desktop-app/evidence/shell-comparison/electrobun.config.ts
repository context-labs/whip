export default {
 app: { name: 'Whip Electrobun Comparison', identifier: 'dev.whip.shellcomparison.d9vagr', version: '0.0.1' },
 build: { mainProcess: 'cottontail', cottontail: { entrypoint: 'comparison-main.ts' },
 copy: { renderer: 'views/bundle' },
 mac: { codesign: false, notarize: false, createDmg: false, bundleCEF: false, bundleWGPU: false, defaultRenderer: 'native' } },
 runtime: { exitOnLastWindowClosed: true }
};
