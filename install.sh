#!/bin/sh
# whipcode installer: verified releases, atomic replacement, no daemon changes.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/context-labs/whip/main/install.sh | sh
# Before v1 stable: download this script, then WHIPCODE_CHANNEL=prerelease sh install.sh
# Requires curl, python3, and sha256sum or shasum.
# Env overrides:
#   WHIPCODE_CHANNEL=prerelease   opt into prereleases (default: stable)
#   WHIPCODE_VERSION=v1.0.0-alpha.1   pin an exact v1+ tag; overrides channel
#   WHIPCODE_BIN_DIR=~/bin        install directory (default: ~/.local/bin)
#   GH_TOKEN=...                 optional GitHub token (authenticated gh also works)
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

# Public releases work anonymously; gh auth or GH_TOKEN raises API limits.
TOKEN=""
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  TOKEN=$(gh auth token)
elif [ -n "${GH_TOKEN:-}" ]; then
  TOKEN="$GH_TOKEN"
fi
api() {
  if [ -n "$TOKEN" ]; then
    curl -fsSL --connect-timeout 10 --max-time 120 -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" "$API/$1"
  else
    curl -fsSL --connect-timeout 10 --max-time 120 -H "Accept: application/vnd.github+json" "$API/$1"
  fi
}
api_asset() {
  if [ -n "$TOKEN" ]; then
    curl -fsSL --connect-timeout 10 --max-time 120 -H "Authorization: Bearer $TOKEN" -H "Accept: application/octet-stream" "$API/releases/assets/$1" -o "$2"
  else
    curl -fsSL --connect-timeout 10 --max-time 120 -H "Accept: application/octet-stream" "$API/releases/assets/$1" -o "$2"
  fi
}
if command -v sha256sum >/dev/null 2>&1; then SHA() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then SHA() { shasum -a 256 "$1" | cut -d' ' -f1; }
else die "need sha256sum or shasum"; fi

os=$(uname -s); arch=$(uname -m)
case "$os" in Linux) os=linux;; Darwin) os=darwin;; *) die "unsupported OS: $os (Linux/macOS only)";; esac
case "$arch" in x86_64|amd64) arch=x64;; arm64|aarch64) arch=arm64;; *) die "unsupported arch: $arch";; esac
ASSET="whipcode-$os-$arch"

# Never use /releases/latest: historical products share this repository.
# Scan bounded pages, require the complete new asset set, and order by semver.
tmp=$(mktemp -d)
staged=""
cleanup() { rm -rf "$tmp"; [ -z "$staged" ] || rm -f "$staged"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
VERSION="${WHIPCODE_VERSION:-}"
pinned="$VERSION"
CHANNEL="${WHIPCODE_CHANNEL:-stable}"
case "$CHANNEL" in stable|prerelease) ;; *) die "WHIPCODE_CHANNEL must be stable or prerelease";; esac

# Share the parser/comparator between discovery, pin validation and downgrade
# protection. Build metadata does not affect SemVer precedence.
cat > "$tmp/releases.py" <<'PYTHON'
import json, re, sys


def version(tag):
    if not isinstance(tag, str):
        return None
    match = re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?", tag)
    if not match or int(match[1]) < 1:
        return None
    pre = match[4]
    identifiers = []
    for part in pre.split(".") if pre else []:
        if part.isdigit():
            if len(part) > 1 and part[0] == "0":
                return None
            identifiers.append((0, int(part)))
        else:
            identifiers.append((1, part))
    return (*map(int, match.group(1, 2, 3)), pre is None, tuple(identifiers))


# Match the existing publisher's bounded, flat ASCII asset names.
asset_name = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,199}")
required = {"whipcode-linux-x64", "whipcode-linux-arm64", "whipcode-darwin-x64",
            "whipcode-darwin-arm64", "SHA256SUMS", "install.sh"}

mode, *args = sys.argv[1:]
if mode == "pin":
    if args[0] and version(args[0]) is None:
        sys.exit("WHIPCODE_VERSION must be an exact v1+ SemVer tag (for example v1.0.0-alpha.1)")
elif mode == "checksum":
    source, asset = args
    checksums = {}
    with open(source, encoding="ascii", newline="") as f:
        for line in f:
            match = re.fullmatch(r"([0-9a-fA-F]{64}) [ *]([^\r\n]+)\n?", line)
            if not match or not asset_name.fullmatch(match[2]) or match[2] in checksums:
                sys.exit("invalid or duplicate SHA256SUMS entry")
            checksums[match[2]] = match[1].lower()
    if not (required - {"SHA256SUMS"}) <= checksums.keys():
        sys.exit("missing required CLI checksum in SHA256SUMS")
    print(checksums[asset])
elif mode == "newer":
    current = args[0].strip()
    if current.startswith("whipcode "):
        current = current[len("whipcode "):]
    parsed = version(current)
    sys.exit(bool(parsed and parsed > version(args[1])))
else:
    source, selection, asset, pinned, channel = args
    with open(source) as f:
        data = json.load(f)
    releases = [data] if pinned else data
    if not isinstance(releases, list):
        sys.exit("invalid release list")
    with open(selection) as f:
        best = json.load(f)
    for release in releases:
        if not isinstance(release, dict):
            continue
        tag = release.get("tag_name")
        parsed = version(tag)
        if parsed is None or release.get("draft") is not False:
            continue
        # The release's channel must agree with its tag, not GitHub's ordering.
        if release.get("prerelease") is not (not parsed[3]):
            continue
        if pinned and tag != pinned:
            continue
        if not pinned and channel == "stable" and not parsed[3]:
            continue
        raw_assets = release.get("assets", [])
        if not isinstance(raw_assets, list):
            continue
        assets = {a["name"]: a["id"] for a in raw_assets if isinstance(a, dict)
                  and isinstance(a.get("name"), str) and asset_name.fullmatch(a["name"])
                  and type(a.get("id")) is int and a["id"] > 0
                  and type(a.get("size")) is int and a["size"] > 0}
        # CLI consumers require their subset; the publisher validates the full
        # CLI/Desktop/evidence union. Still reject malformed or duplicate extras.
        if (not required <= assets.keys() or len(assets) != len(raw_assets)
                or len(set(assets.values())) != len(assets)):
            continue
        if best is None or parsed > version(best["tag"]):
            best = {"tag": tag, "binary": assets[asset], "sums": assets["SHA256SUMS"]}
    with open(selection, "w") as f:
        json.dump(best, f)
    print(len(releases), *([best["tag"], best["binary"], best["sums"]] if best else ["-", 0, 0]))
PYTHON

# Reject foreign tags before incorporating the pin in an API path.
python3 "$tmp/releases.py" pin "$pinned" || die "invalid version pin"
say "Resolving ${VERSION:-latest $CHANNEL} release..."
printf 'null\n' > "$tmp/selection.json"
page=1
while :; do
  if [ -n "$pinned" ]; then
    api "releases/tags/$pinned" > "$tmp/release.json" || die "release $pinned not found"
  else
    api "releases?per_page=100&page=$page" > "$tmp/release.json" || die "release lookup failed"
  fi
  parsed=$(python3 "$tmp/releases.py" select "$tmp/release.json" "$tmp/selection.json" "$ASSET" "$pinned" "$CHANNEL") \
    || die "could not parse releases"
  # Validated numeric IDs and a whitespace-free SemVer tag, never shell code.
  read -r count VERSION BIN_ID SUMS_ID <<FIELDS
$parsed
FIELDS
  [ -z "$pinned" ] && [ "$count" -eq 100 ] || break
  [ "$page" -lt 100 ] || die "release lookup exceeded 100 pages; set WHIPCODE_VERSION"
  page=$((page + 1))
done
[ "$BIN_ID" -gt 0 ] || die "no complete ${pinned:-$CHANNEL} whipcode v1+ release found; before v1 stable, explicitly set WHIPCODE_CHANNEL=prerelease"

printf '\nwhipcode %s — %s\n' "$VERSION" "$ASSET"
say "Downloading $ASSET..."
api_asset "$BIN_ID" "$tmp/$ASSET" || die "download failed"
say "Downloading SHA256SUMS..."
api_asset "$SUMS_ID" "$tmp/SHA256SUMS" || die "checksum list download failed"
expected=$(python3 "$tmp/releases.py" checksum "$tmp/SHA256SUMS" "$ASSET") \
  || die "invalid SHA256SUMS; refusing to install"
actual=$(SHA "$tmp/$ASSET")
say "expected sha256: $expected"
say "actual   sha256: $actual"
[ "$expected" = "$actual" ] || die "CHECKSUM MISMATCH — refusing to install. The download does not match the published checksum."
say "OK: checksum verified"
chmod 755 "$tmp/$ASSET"
downloaded=$("$tmp/$ASSET" --version) || die "downloaded binary could not run"
[ "$downloaded" = "whipcode $VERSION" ] || die "downloaded binary version does not match $VERSION"

# User-local by default: never guess a global install directory from PATH.
in_path() { case ":$PATH:" in *":$1:"*) return 0;; *) return 1;; esac; }
DEST="${WHIPCODE_BIN_DIR:-$HOME/.local/bin}"
mkdir -p "$DEST" || die "could not create install directory"
DEST=$(cd "$DEST" && pwd -P) || die "could not resolve install directory"
[ -w "$DEST" ] || die "no writable install directory found"
[ ! -d "$DEST/whipcode" ] || die "install destination is a directory: $DEST/whipcode"

# Only an explicit pin permits a rollback within the new product.
if [ -z "$pinned" ] && [ -x "$DEST/whipcode" ]; then
  current=$("$DEST/whipcode" --version 2>/dev/null || true)
  python3 "$tmp/releases.py" newer "$current" "$VERSION" \
    || die "installed whipcode is newer; set WHIPCODE_VERSION for an explicit rollback"
fi
# Stage on the destination filesystem so replacement is atomic.
staged=$(mktemp "$DEST/.whipcode.XXXXXX") || die "could not stage installation"
cp "$tmp/$ASSET" "$staged" || die "could not stage binary"
chmod 755 "$staged"
mv -f "$staged" "$DEST/whipcode" || die "could not replace whipcode"
staged=""
say "OK: installed to $DEST/whipcode"

if ! in_path "$DEST"; then
  # Quote literal destinations, including spaces and shell metacharacters.
  esc=$(printf '%s' "$DEST" | sed "s/'/'\\\\''/g")
  line="export PATH='$esc':\"\$PATH\""
  touch "$HOME/.profile" 2>/dev/null || true
  for rc in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.profile"; do
    [ -e "$rc" ] || continue
    grep -qF "$line" "$rc" 2>/dev/null || printf '\n# whipcode\n%s\n' "$line" >> "$rc"
  done
  say "Added $DEST to your PATH — restart your shell, or run now: $line"
fi
printf '\n'
"$DEST/whipcode" --version || true
if [ "$os" = "darwin" ]; then
  say "macOS: allow whipcode in Privacy & Security if Gatekeeper blocks it."
  say "Native computer control needs Accessibility and Screen Recording permissions."
fi
# shellcheck disable=SC2016
printf '\nDone. Run `whipcode` to start.\n'
