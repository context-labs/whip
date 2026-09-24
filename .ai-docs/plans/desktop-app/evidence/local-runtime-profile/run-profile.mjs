import {spawn} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const env={};
for(const key of ['HOME','USER','LOGNAME','TMPDIR'])if(process.env[key])env[key]=process.env[key];
env.PATH='/usr/bin:/bin:/usr/sbin:/sbin';env.SHELL='/bin/zsh';
env.WHIP_COMPARISON_REPOSITORY=process.env.WHIP_COMPARISON_REPOSITORY || fileURLToPath(new URL('../../../../../',import.meta.url));
if(process.env.WHIP_RUNTIME_PROFILE_OUTPUT)env.WHIP_RUNTIME_PROFILE_OUTPUT=process.env.WHIP_RUNTIME_PROFILE_OUTPUT;
const child=spawn(process.execPath,[fileURLToPath(new URL('./profile.mjs',import.meta.url))],{env,stdio:'inherit'});
await new Promise(resolve=>child.on('exit',code=>{process.exitCode=code;resolve();}));
