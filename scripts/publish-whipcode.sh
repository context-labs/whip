#!/usr/bin/env bash
# Invoked only after the exact source commit passed the branch release gates.
set -euo pipefail
: "${SOURCE_SHA:?}" "${RELEASE_TAG:?}" "${GH_REPO:?}"
die() { echo "$*" >&2; exit 1; }
[[ "$GH_REPO" == context-labs/whip ]] || die "Unexpected repository: $GH_REPO"
[[ "$RELEASE_TAG" =~ ^whipcode-v0\.0\.[1-9][0-9]*$ ]] || die "Invalid release tag"
[[ "$SOURCE_SHA" =~ ^[0-9a-f]{40}$ ]] || die "Invalid source SHA"

head=$(gh api "repos/$GH_REPO/git/ref/heads/whip-rlm" --jq .object.sha)
if [[ "$head" != "$SOURCE_SHA" ]]; then
  echo "Skipping superseded source $SOURCE_SHA (branch is $head)."
  exit 0
fi

assets=(whipcode-linux-x64 whipcode-linux-arm64 whipcode-darwin-x64 whipcode-darwin-arm64)
for asset in "${assets[@]}"; do test -s "artifacts/$asset"; done
[[ $(find artifacts -maxdepth 1 -type f -name 'whipcode-*' | wc -l) -eq 4 ]]
(cd artifacts && sha256sum "${assets[@]}" > SHA256SUMS)
cp install-whipcode.sh artifacts/

# A draft may not have a git tag until publication. Verify a tag independently
# when present: --target cannot override a tag left behind by a deleted release.
tag_exists=false
if git ls-remote --exit-code origin "refs/tags/$RELEASE_TAG" >/dev/null; then
  tag_exists=true
  git fetch --no-tags origin "refs/tags/$RELEASE_TAG"
  [[ $(git rev-parse 'FETCH_HEAD^{commit}') == "$SOURCE_SHA" ]] || die "Release tag points to another commit"
else
  status=$?
  [[ "$status" -eq 2 ]] || die "Could not inspect release tag (git status $status)"
fi

if gh release view "$RELEASE_TAG" --json isDraft,targetCommitish,assets > artifacts/release.json 2>/dev/null; then
  draft=$(python3 -c 'import json; print(json.load(open("artifacts/release.json"))["isDraft"])')
  if [[ "$draft" == False ]]; then
    [[ "$tag_exists" == true ]] || die "Published release has no tag"
    # Rebuild bytes (especially Swift signatures) need not be identical. Verify
    # the published complete set against its own manifest without clobbering it.
    gh release download "$RELEASE_TAG" --pattern 'whipcode-*' --pattern SHA256SUMS \
      --pattern install-whipcode.sh --dir artifacts/published
    for asset in "${assets[@]}" SHA256SUMS install-whipcode.sh; do test -s "artifacts/published/$asset"; done
    (cd artifacts/published && sha256sum --check SHA256SUMS)
    cmp install-whipcode.sh artifacts/published/install-whipcode.sh
    echo "Release $RELEASE_TAG is already published and verified."
    exit 0
  fi
  if [[ "$tag_exists" == false ]]; then
    target=$(python3 -c 'import json; print(json.load(open("artifacts/release.json"))["targetCommitish"])')
    [[ "$target" == "$SOURCE_SHA" ]] || die "Draft release targets another commit"
  fi
else
  # shellcheck disable=SC2016 # Backticks format literal Markdown.
  printf 'Whipcode branch build from `whip-rlm`.\n\nSource commit: `%s`\n\nInstall:\n```sh\ncurl -fsSL https://raw.githubusercontent.com/context-labs/whip/whip-rlm/install-whipcode.sh | sh\n```\n' "$SOURCE_SHA" > artifacts/notes.md
  gh release create "$RELEASE_TAG" --target "$SOURCE_SHA" --draft --prerelease --latest=false \
    --title "whipcode ${RELEASE_TAG#whipcode-}" --notes-file artifacts/notes.md
fi

# Only an unpublished draft may be repaired with clobber.
gh release upload "$RELEASE_TAG" artifacts/whipcode-* artifacts/SHA256SUMS artifacts/install-whipcode.sh --clobber
gh release view "$RELEASE_TAG" --json assets > artifacts/release.json
python3 - <<'PY'
import json
required = {'whipcode-linux-x64', 'whipcode-linux-arm64', 'whipcode-darwin-x64',
            'whipcode-darwin-arm64', 'SHA256SUMS', 'install-whipcode.sh'}
assets = json.load(open('artifacts/release.json'))['assets']
assert required.issubset({a['name'] for a in assets if a['size'] > 0}), 'missing release assets'
PY
# Supersession during uploads leaves a resumable draft, never a partial release.
head=$(gh api "repos/$GH_REPO/git/ref/heads/whip-rlm" --jq .object.sha)
if [[ "$head" != "$SOURCE_SHA" ]]; then
  echo "Leaving superseded build $RELEASE_TAG as a draft."
  exit 0
fi
gh release edit "$RELEASE_TAG" --draft=false --prerelease --latest=false
