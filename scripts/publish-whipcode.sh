#!/usr/bin/env bash
# One publication owner: exact candidate -> GitHub draft -> CDN stage -> GitHub -> feed.
set -euo pipefail
: "${SOURCE_SHA:?}" "${RELEASE_TAG:?}" "${RELEASE_MODE:?}" "${GH_REPO:?}"
die() { echo "$*" >&2; exit 1; }
[[ "${WHIP_RELEASE_ENABLED:-}" == true ]] || die "Publication is disabled"
[[ "$GH_REPO" == context-labs/whip && "${GITHUB_REPOSITORY:-}" == "$GH_REPO" ]] || die "Unexpected repository"
[[ "$SOURCE_SHA" =~ ^[0-9a-f]{40}$ ]] || die "Invalid source SHA"
[[ "${WHIP_RELEASE_BASELINE:-}" =~ ^[0-9a-f]{40}$ ]] || die "Missing clean release baseline"
base='[1-9][0-9]*\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)'
case "$RELEASE_MODE" in
  alpha) [[ "$RELEASE_TAG" =~ ^v$base-alpha\.[1-9][0-9]*$ ]] || die "Invalid alpha tag" ;;
  stable) [[ "$RELEASE_TAG" =~ ^v$base$ ]] || die "Invalid stable tag" ;;
  *) die "Invalid release mode" ;;
esac
candidate=${1:-candidate}
[[ -d "$candidate" ]] || die "Missing validated candidate directory"

# vars gate admission, not live cancellation. Disable this workflow AND cancel
# active runs to stop operations. A paused candidate remains valid as main advances.
require_release_authority() {
  local workflow_state
  workflow_state=$(gh api "repos/$GH_REPO/actions/workflows/publish-cli.yml" --jq .state) || die "Could not verify release workflow state"
  [[ "$workflow_state" == active ]] || die "Release workflow is disabled"
  [[ $(git rev-parse HEAD) == "$SOURCE_SHA" ]] || die "Checkout differs from validated source"
  git fetch --no-tags origin refs/heads/main:refs/remotes/origin/main || die "Could not refresh protected main"
  git merge-base --is-ancestor "$WHIP_RELEASE_BASELINE" "$SOURCE_SHA" || die "Source predates clean release baseline"
  git merge-base --is-ancestor "$SOURCE_SHA" origin/main || die "Source is not in protected main history"
}
require_release_authority
node apps/desktop/scripts/release-candidate.mjs verify "$candidate"
cmp install.sh "$candidate/install.sh"

# Explicit GITHUB_TOKEN tag creation, not tag-trigger chaining. Never retarget.
if git ls-remote --exit-code origin "refs/tags/$RELEASE_TAG" >/dev/null; then
  git fetch --no-tags origin "refs/tags/$RELEASE_TAG"
  [[ $(git rev-parse 'FETCH_HEAD^{commit}') == "$SOURCE_SHA" ]] || die "Release tag points to another commit"
else
  status=$?
  [[ "$status" -eq 2 ]] || die "Could not inspect release tag (git status $status)"
  require_release_authority
  gh api --method POST "repos/$GH_REPO/git/refs" -f "ref=refs/tags/$RELEASE_TAG" -f "sha=$SOURCE_SHA"
fi

# The existing JS helper alone owns draft/upload/undraft/latest. It verifies exact
# existing bytes on retries and never clobbers either draft or published content.
require_release_authority
WHIP_DESKTOP_PUBLISH_MODE=stage node apps/desktop/scripts/publish-github.mjs "$candidate"/*
require_release_authority
WHIP_DESKTOP_PUBLISH_MODE=stage node apps/desktop/scripts/publish.mjs "$candidate"
require_release_authority
WHIP_DESKTOP_PUBLISH_MODE=promote node apps/desktop/scripts/publish-github.mjs "$candidate"/*
# GitHub is now public. The feed is deliberately last, but cross-service atomicity
# is impossible; report partial success explicitly and recover these exact bytes.
if ! (
  require_release_authority
  WHIP_DESKTOP_PUBLISH_MODE=promote node apps/desktop/scripts/publish.mjs "$candidate"
); then
  die "GitHub release is published, but Desktop feed promotion failed. Retry this exact candidate; do not rebuild or change its tag."
fi
