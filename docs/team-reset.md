# Internal clean-project reset

This is a **manual checklist**, not an uninstaller. It discards pre-reset WHIP
and WhipCode installations/state; it does not migrate them. Each machine and
remote host needs its owner's explicit approval. Do not run a broad `rm -rf`,
kill by process-name pattern, or delete anything just because its name includes
“whip”. Repository histories and published artifacts are outside this checklist.

## 1. Inventory and confirm

- Record host, owner, active projects, running work, and any exports/backups the
  owner wants to retain. Old stores cannot be restored into the fresh runtime.
- Locate **every actual executable**: use `type -a whip whipcode`, inspect shell
  aliases/functions and symlinks, and inspect Desktop's saved executable path.
  Common locations are `~/.local/bin`, `/usr/local/bin`, `GOBIN`/`GOPATH/bin`,
  checkout `whip`/`whipcode`, and custom paths. Record exact resolved targets.
- List each app: `/Applications/Whip.app`, `/Applications/Whip Beta.app`, user
  Applications copies, development apps, and retained `.whip-local-update-*`
  backups. Confirm which copies belong to this reset.
- List homes: `~/.whip`, `~/.whipcode`, every explicitly configured `WHIP_HOME`
  or `WHIPCODE_HOME`, and checkout `apps/desktop/.dev` fixtures. These can contain
  credentials, config, databases/sessions, artifacts, sockets/locks, browser
  profiles/extension state, skills, downloaded models/icons, and native helpers.
- List Desktop GUI data: `~/Library/Application Support/Whip`, `Whip Beta`,
  `Whip Dev`, plus any `WHIP_DESKTOP_USER_DATA` override. Check matching
  app-specific caches/preferences/saved application state without selecting
  unrelated apps. Inspect their actual bundle IDs (`com.contextlabs.whip`,
  `com.contextlabs.whip.beta`) rather than assuming all matching files are safe.
- Inspect login items, LaunchAgents/LaunchDaemons, user/system systemd units,
  containers, cron jobs, shell startup files, aliases, and PATH entries for
  processes that restart the old executable. Record exact service IDs/files.
  Repeat on SSH/URL hosts; Desktop does not manage their upgrades.
- In each browser, identify only WHIP gateway-origin site storage/service workers
  and the installed WHIP extension. Identify old mobile app host/setup state if
  used. Do not clear all browser data or other tools' credentials/skills.

**Deletion confirmation:** before proceeding, obtain the owner's recorded
approval of the exact paths, services, browser origins/extension IDs, and devices
above. Confirm exports are complete and that sessions, credentials, and settings
in those paths will be lost. Any newly discovered target needs separate approval.

## 2. Stop and remove only approved targets

1. Finish/stop work deliberately and quit every Desktop/TUI/web/mobile client.
2. For each recorded old executable/home, stop its daemon using that executable
   and its own home environment. Stop recorded gateway processes and service
   supervisors. Verify the exact PIDs exited and listeners/sockets are no longer
   active; do not delete live state or signal unrelated processes.
3. Disable/remove only approved startup services, shell aliases/env entries, and
   PATH links. Remove the approved binaries, apps, homes, GUI data, and backups
   individually. Do not remove repository `.git`, source projects, system signing
   certificates, Apple permissions, SSH keys, provider key files, `~/.agents`, or
   another tool's state. Revoke credentials only with their owner's authorization.
4. Clear only the approved browser-origin/extension and mobile setup state.
   Remove remote-host installations separately with the remote owner's consent.
5. Recheck executable resolution and service status. No old daemon or launcher
   should remain. Record what was removed and any deliberately retained item.

## 3. Fresh installation and acceptance

- Wait for a release built from the recorded clean release baseline. Use
  [setup](setup.md) for the sole CLI (`main/install.sh`, explicit prerelease
  before v1 stable), or install the newly approved `desktop-v*` app and choose
  **Set up this Mac**. Do not reuse a pre-reset app or restore its saved state.
- Confirm `whipcode --version`, selected executable, `~/.whipcode` (or a fresh
  `WHIPCODE_HOME`), and `whipcode daemon status --json`. Start a new session,
  configure providers, and verify socket and web access. No listener is opened
  unless explicitly requested.
- Desktop-installed backends update with Desktop; standalone backends use
  `whipcode update`. For remote pairing, install the exact Desktop release's
  matching backend and verify checksums/attestation as described in
  [Desktop releases](desktop-releases.md#matching-remote-backend).
- Record host, installed tag/source, ownership, acceptance results, and remaining
  exceptions. The reset is complete only after the owner confirms this list.
