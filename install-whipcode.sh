#!/bin/sh
# whipcode installer — downloads the released binary, verifies its checksum
# against the published SHA256SUMS, and installs it to a directory on your PATH.
#
# Everything it does is printed as it happens. Read it first if you like:
#   curl -fsSL --connect-timeout 10 --max-time 120 https://raw.githubusercontent.com/context-labs/whip/whip-rlm/install-whipcode.sh
#
# Usage:
#   curl -fsSL --connect-timeout 10 --max-time 120 https://raw.githubusercontent.com/context-labs/whip/whip-rlm/install-whipcode.sh | sh
# Requires curl, python3, and sha256sum or shasum.
# Env overrides:
#   WHIPCODE_VERSION=whipcode-v0.0.1   pin a tag, e.g. whipcode-v0.0.1 (default: latest whipcode)
#   WHIPCODE_BIN_DIR=~/bin    force the install directory
#   GH_TOKEN=...           optional GitHub token for a higher API rate limit
#                          (authenticated `gh` also works)
set -eu

REPO="context-labs/whip"
API="https://api.github.com/repos/$REPO"

say() { printf '  %s\n' "$*"; }
die() { printf 'whipcode install: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"; }

need uname
need mktemp
need curl
need python3

# --- auth ---
# Public releases work anonymously. Prefer the `gh` CLI's stored auth when
# available, then GH_TOKEN, to use the authenticated API rate limit.
TOKEN=""
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  TOKEN=$(gh auth token)
elif [ -n "${GH_TOKEN:-}" ]; then
  TOKEN="$GH_TOKEN"
fi
api() { # api <path> — JSON on stdout
  if [ -n "$TOKEN" ]; then
    curl -fsSL --connect-timeout 10 --max-time 120 -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" "$API/$1"
  else
    curl -fsSL --connect-timeout 10 --max-time 120 -H "Accept: application/vnd.github+json" "$API/$1"
  fi
}
api_asset() { # api_asset <id> <outfile> — binary download
  if [ -n "$TOKEN" ]; then
    curl -fsSL --connect-timeout 10 --max-time 120 -H "Authorization: Bearer $TOKEN" -H "Accept: application/octet-stream" "$API/releases/assets/$1" -o "$2"
  else
    curl -fsSL --connect-timeout 10 --max-time 120 -H "Accept: application/octet-stream" "$API/releases/assets/$1" -o "$2"
  fi
}

# a sha256 tool
if command -v sha256sum >/dev/null 2>&1; then SHA() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then SHA() { shasum -a 256 "$1" | cut -d' ' -f1; }
else die "need sha256sum or shasum"; fi

# --- platform ---
os=$(uname -s); arch=$(uname -m)
case "$os" in Linux) os=linux;; Darwin) os=darwin;; *) die "unsupported OS: $os (Linux/macOS only)";; esac
case "$arch" in x86_64|amd64) arch=x64;; arm64|aarch64) arch=arm64;; *) die "unsupported arch: $arch";; esac
ASSET="whipcode-$os-$arch"

# --- release selection ---
# Prereleases are excluded by /releases/latest. Scan bounded pages and compare
# the channel's numeric build counter, never list order or stable whip versions.
tmp=$(mktemp -d)
staged=""
cleanup() { rm -rf "$tmp"; [ -z "$staged" ] || rm -f "$staged"; }
trap cleanup EXIT
VERSION="${WHIPCODE_VERSION:-}"
pinned="$VERSION"
parse_release() {
  python3 - "$tmp/release.json" "$ASSET" "$pinned" <<'PYTHON'
import json, re, sys
with open(sys.argv[1]) as f:
    data = json.load(f)
asset, pinned = sys.argv[2:]
pattern = r"whipcode-v0\.0\.([1-9][0-9]*)"
if pinned and not re.fullmatch(pattern, pinned):
    sys.exit("WHIPCODE_VERSION must be an exact whipcode-v0.0.N tag")
releases = [data] if pinned else data
if not isinstance(releases, list):
    sys.exit("invalid release list")
required = {"whipcode-linux-x64", "whipcode-linux-arm64", "whipcode-darwin-x64",
            "whipcode-darwin-arm64", "SHA256SUMS", "install-whipcode.sh"}
best = (0, "-", 0, 0)
for release in releases:
    tag = release.get("tag_name", "")
    match = re.fullmatch(pattern, tag)
    if not match or release.get("draft") or release.get("prerelease") is not True:
        continue
    if pinned and tag != pinned:
        continue
    assets = {a["name"]: a["id"] for a in release.get("assets", [])
              if isinstance(a.get("id"), int) and a["id"] > 0 and a.get("size", 0) > 0}
    if not required.issubset(assets):
        continue
    number = int(match[1])
    if number > 9223372036854775807:
        sys.exit("release counter out of range")
    best = max(best, (number, tag, assets[asset], assets["SHA256SUMS"]))
print(len(releases), *best)
PYTHON
}

# Reject foreign tags before incorporating the pin in an API path.
if [ -n "$pinned" ]; then
  python3 -c 'import re,sys; sys.exit(not bool(re.fullmatch(r"whipcode-v0\.0\.[1-9][0-9]*",sys.argv[1])))' "$pinned" \
    || die "WHIPCODE_VERSION must be an exact whipcode-v0.0.N tag"
fi
say "Resolving ${VERSION:-latest whipcode} release..."
page=1
best=0
BIN_ID=""
SUMS_ID=""
while :; do
  if [ -n "$pinned" ]; then
    api "releases/tags/$pinned" > "$tmp/release.json" || die "release $pinned not found"
  else
    api "releases?per_page=100&page=$page" > "$tmp/release.json" || die "release lookup failed"
  fi
  parsed=$(parse_release) || die "could not parse releases"
  # All five fields are validated numbers or a fixed-format tag; no whitespace.
  read -r count number tag bin_id sums_id <<FIELDS
$parsed
FIELDS
  if [ "$number" -gt "$best" ]; then
    best=$number; VERSION=$tag; BIN_ID=$bin_id; SUMS_ID=$sums_id
  fi
  [ -z "$pinned" ] && [ "$count" -eq 100 ] || break
  [ "$page" -lt 100 ] || die "release lookup exceeded 100 pages; set WHIPCODE_VERSION"
  page=$((page + 1))
done
[ -n "$BIN_ID" ] || die "no complete whipcode release found"

printf '\nwhipcode %s — %s\n' "$VERSION" "$ASSET"

# --- download + verify ---
say "Downloading $ASSET..."
api_asset "$BIN_ID" "$tmp/$ASSET" || die "download failed"
say "Downloading SHA256SUMS..."
api_asset "$SUMS_ID" "$tmp/SHA256SUMS" || die "checksum list download failed"

expected=$(grep " $ASSET\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)
[ -n "$expected" ] || die "no checksum for $ASSET in SHA256SUMS"
actual=$(SHA "$tmp/$ASSET")
say "expected sha256: $expected"
say "actual   sha256: $actual"
[ "$expected" = "$actual" ] || die "CHECKSUM MISMATCH — refusing to install. The download does not match the published checksum."
say "OK: checksum verified"
chmod 755 "$tmp/$ASSET"

# --- pick an install dir on PATH (first writable wins; create ~/.local/bin if needed) ---
in_path() { case ":$PATH:" in *":$1:"*) return 0;; *) return 1;; esac; }
DEST=""
if [ -n "${WHIPCODE_BIN_DIR:-}" ]; then
  mkdir -p "$WHIPCODE_BIN_DIR" 2>/dev/null || true
  DEST="$WHIPCODE_BIN_DIR"
else
  for d in /usr/local/bin /opt/homebrew/bin "$HOME/.local/bin" "$HOME/bin"; do
    if [ -d "$d" ] && [ -w "$d" ]; then DEST="$d"; break; fi
  done
  # nothing writable existed — create the standard user dir
  [ -z "$DEST" ] && { mkdir -p "$HOME/.local/bin" && DEST="$HOME/.local/bin"; }
fi
[ -n "$DEST" ] && [ -w "$DEST" ] || die "no writable install directory found"

# An automatic install must not replace a newer local release with an older one.
if [ -z "$pinned" ] && [ -x "$DEST/whipcode" ]; then
  current=$("$DEST/whipcode" --version 2>/dev/null || true)
  python3 -c 'import re,sys
m=re.fullmatch(r"whipcode v0\.0\.([1-9][0-9]*)",sys.argv[1].strip())
sys.exit(bool(m and int(m[1]) > int(sys.argv[2])))' "$current" "$best" \
    || die "installed whipcode is newer; set WHIPCODE_VERSION for an explicit rollback"
fi
# Stage on the destination filesystem so replacement is atomic.
staged=$(mktemp "$DEST/.whipcode.XXXXXX") || die "could not stage installation"
cp "$tmp/$ASSET" "$staged" || die "could not stage binary"
chmod 755 "$staged"
mv -f "$staged" "$DEST/whipcode" || die "could not replace whipcode"
staged=""
say "OK: installed to $DEST/whipcode"

# --- ensure it's on PATH ---
if ! in_path "$DEST"; then
  # Single-quote DEST (escaping embedded quotes) so a directory with spaces or
  # shell metacharacters can't inject code when the rc file is sourced.
  esc=$(printf '%s' "$DEST" | sed "s/'/'\\\\''/g")
  line="export PATH='$esc':\"\$PATH\""
  # Update every shell rc that exists, and always ~/.profile (create it if
  # missing) so a fresh machine still gets PATH on next login.
  touch "$HOME/.profile" 2>/dev/null || true
  for rc in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.profile"; do
    [ -e "$rc" ] || continue
    grep -qF "$line" "$rc" 2>/dev/null || printf '\n# whipcode\n%s\n' "$line" >> "$rc"
  done
  say "Added $DEST to your PATH — restart your shell, or run now: $line"
fi

printf '\n'
"$DEST/whipcode" --version || true

if [ "$os" = "darwin" ] && [ "$arch" = "arm64" ]; then
  cat <<'EOF'

macOS notes:
  • First run may trigger Gatekeeper — if "whipcode" is blocked, allow it in
    System Settings → Privacy & Security, or run: xattr -d com.apple.quarantine $(which whipcode)
  • computer_exec's native tier (driving apps) needs Accessibility + Screen
    Recording granted to your terminal app when prompted.
EOF
fi

# shellcheck disable=SC2016 # Backticks are literal command formatting.
printf '\nDone. Run `whipcode` to start.\n'
