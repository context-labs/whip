#!/usr/bin/env bash
# Invoked after this exact main commit passed the shared CI/security gates.
set -euo pipefail
: "${SOURCE_SHA:?}" "${RELEASE_TAG:?}" "${RELEASE_MODE:?}" "${GH_REPO:?}"
die() { echo "$*" >&2; exit 1; }
[[ "${WHIP_RELEASE_ENABLED:-}" == true ]] || die "Publication is disabled"
[[ "$GH_REPO" == context-labs/whip ]] || die "Unexpected repository: $GH_REPO"
[[ "$SOURCE_SHA" =~ ^[0-9a-f]{40}$ ]] || die "Invalid source SHA"
[[ "${WHIP_RELEASE_BASELINE:-}" =~ ^[0-9a-f]{40}$ ]] || die "Missing clean release baseline"
case "$RELEASE_MODE" in
  alpha) [[ "$RELEASE_TAG" =~ ^v1\.0\.0-alpha\.[1-9][0-9]*$ ]] || die "Invalid alpha tag" ;;
  stable) [[ "$RELEASE_TAG" == v1.0.0 ]] || die "Invalid stable tag" ;;
  *) die "Invalid release mode" ;;
esac
[[ $(git rev-parse HEAD) == "$SOURCE_SHA" ]] || die "Checkout differs from validated source"
git merge-base --is-ancestor "$WHIP_RELEASE_BASELINE" "$SOURCE_SHA" || die "Source predates clean release baseline"

# vars are an admission gate, not live cancellation. Disabling this workflow
# and cancelling active runs is the operational stop; fail closed on API errors.
require_active_workflow() {
  local workflow_state
  workflow_state=$(gh api "repos/$GH_REPO/actions/workflows/publish-cli.yml" --jq .state) || die "Could not verify release workflow state"
  [[ "$workflow_state" == active ]] || die "Release workflow is disabled"
}
require_active_workflow

head=$(gh api "repos/$GH_REPO/git/ref/heads/main" --jq .object.sha)
if [[ "$head" != "$SOURCE_SHA" ]]; then
  echo "Skipping superseded source $SOURCE_SHA (main is $head)."
  exit 0
fi

assets=(whipcode-linux-x64 whipcode-linux-arm64 whipcode-darwin-x64 whipcode-darwin-arm64)
for asset in "${assets[@]}"; do test -s "artifacts/$asset"; done
[[ $(find artifacts -maxdepth 1 -type f -name 'whipcode-*' | wc -l) -eq 4 ]]
cp install.sh artifacts/
(cd artifacts && sha256sum "${assets[@]}" install.sh > SHA256SUMS)

# A tag left behind by a deleted release must never silently retarget a build.
tag_exists=false
if git ls-remote --exit-code origin "refs/tags/$RELEASE_TAG" >/dev/null; then
  tag_exists=true
  git fetch --no-tags origin "refs/tags/$RELEASE_TAG"
  [[ $(git rev-parse 'FETCH_HEAD^{commit}') == "$SOURCE_SHA" ]] || die "Release tag points to another commit"
else
  status=$?
  [[ "$status" -eq 2 ]] || die "Could not inspect release tag (git status $status)"
fi

if gh release view "$RELEASE_TAG" --json isDraft,isPrerelease,targetCommitish,assets > artifacts/release.json 2>/dev/null; then
  draft=$(python3 -c 'import json; print(json.load(open("artifacts/release.json"))["isDraft"])')
  if [[ "$draft" == False ]]; then
    [[ "$tag_exists" == true ]] || die "Published release has no tag"
    python3 - <<'PY'
import json, os
release = json.load(open('artifacts/release.json'))
assert release['isPrerelease'] == (os.environ['RELEASE_MODE'] == 'alpha'), 'release channel mismatch'
required = {'whipcode-linux-x64', 'whipcode-linux-arm64', 'whipcode-darwin-x64',
            'whipcode-darwin-arm64', 'SHA256SUMS', 'install.sh'}
assert len(release['assets']) == len(required), 'unexpected published assets'
assert {a['name'] for a in release['assets'] if a['size'] > 0} == required, 'missing published assets'
PY
    # Rebuild bytes (notably Swift signatures) need not match. Verify the
    # immutable published set against its own manifest; never clobber it.
    gh release download "$RELEASE_TAG" --pattern 'whipcode-*' --pattern SHA256SUMS \
      --pattern install.sh --dir artifacts/published
    for asset in "${assets[@]}" SHA256SUMS install.sh; do test -s "artifacts/published/$asset"; done
    python3 - <<'PY'
import re
from pathlib import Path
required = {'whipcode-linux-x64', 'whipcode-linux-arm64', 'whipcode-darwin-x64',
            'whipcode-darwin-arm64', 'install.sh'}
lines = Path('artifacts/published/SHA256SUMS').read_text().splitlines()
assert len(lines) == len(required), 'incorrect checksum count'
assert all(re.fullmatch(r'[0-9a-f]{64}  [a-z0-9.-]+', line) for line in lines), 'invalid checksum manifest'
assert {line[66:] for line in lines} == required, 'incorrect checksum assets'
PY
    (cd artifacts/published && sha256sum --check SHA256SUMS)
    cmp install.sh artifacts/published/install.sh
    echo "Release $RELEASE_TAG is already published and verified."
    exit 0
  fi
  if [[ "$tag_exists" == false ]]; then
    target=$(python3 -c 'import json; print(json.load(open("artifacts/release.json"))["targetCommitish"])')
    [[ "$target" == "$SOURCE_SHA" ]] || die "Draft release targets another commit"
  fi
else
  require_active_workflow
  # shellcheck disable=SC2016 # Backticks format literal Markdown.
  printf 'Whipcode release from `main`.\n\nSource commit: `%s`\n\nInstall:\n```sh\ncurl -fsSL https://raw.githubusercontent.com/context-labs/whip/main/install.sh | WHIPCODE_VERSION=%s sh\n```\n' "$SOURCE_SHA" "$RELEASE_TAG" > artifacts/notes.md
  # Drafts never become latest, including an approved stable candidate.
  gh release create "$RELEASE_TAG" --target "$SOURCE_SHA" --draft --prerelease --latest=false \
    --title "whipcode $RELEASE_TAG" --notes-file artifacts/notes.md
fi

# Only an unpublished draft may be repaired with clobber.
require_active_workflow
gh release upload "$RELEASE_TAG" artifacts/whipcode-* artifacts/SHA256SUMS artifacts/install.sh --clobber
gh release view "$RELEASE_TAG" --json assets > artifacts/release.json
python3 - <<'PY'
import json
required = {'whipcode-linux-x64', 'whipcode-linux-arm64', 'whipcode-darwin-x64',
            'whipcode-darwin-arm64', 'SHA256SUMS', 'install.sh'}
assets = json.load(open('artifacts/release.json'))['assets']
assert len(assets) == len(required), 'unexpected release assets'
assert required == {a['name'] for a in assets if a['size'] > 0}, 'missing release assets'
PY
# Supersession during uploads leaves a resumable draft, never a partial release.
head=$(gh api "repos/$GH_REPO/git/ref/heads/main" --jq .object.sha)
if [[ "$head" != "$SOURCE_SHA" ]]; then
  echo "Leaving superseded build $RELEASE_TAG as a draft."
  exit 0
fi
require_active_workflow
# GITHUB_TOKEN-created tags do not trigger workflows: this job explicitly
# creates the ref and publishes the already validated assets, without chaining.
if [[ "$tag_exists" == false ]]; then
  gh api --method POST "repos/$GH_REPO/git/refs" -f "ref=refs/tags/$RELEASE_TAG" -f "sha=$SOURCE_SHA"
fi
if [[ "$RELEASE_MODE" == stable ]]; then
  gh release edit "$RELEASE_TAG" --draft=false --prerelease=false --latest=true
else
  gh release edit "$RELEASE_TAG" --draft=false --prerelease --latest=false
fi
