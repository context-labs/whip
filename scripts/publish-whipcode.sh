#!/usr/bin/env bash
# One publication owner: exact candidate -> GitHub draft -> CDN stage -> GitHub -> feed.
set -euo pipefail
: "${SOURCE_SHA:?}" "${SOURCE_REF:?}" "${RELEASE_TAG:?}" "${RELEASE_MODE:?}" "${GH_REPO:?}"
die() { echo "$*" >&2; exit 1; }
[[ "${WHIP_RELEASE_ENABLED:-}" == true ]] || die "Publication is disabled"
[[ "$GH_REPO" == context-labs/whip && "${GITHUB_REPOSITORY:-}" == "$GH_REPO" ]] || die "Unexpected repository"
[[ "$SOURCE_SHA" =~ ^[0-9a-f]{40}$ && "$SOURCE_SHA" == "${GITHUB_SHA:-}" ]] || die "Invalid source SHA"
case "${GITHUB_EVENT_NAME:-}:${GITHUB_REF:-}:$RELEASE_MODE" in
  push:refs/heads/development:alpha|workflow_dispatch:refs/heads/development:alpha) source_branch=development ;;
  workflow_dispatch:refs/heads/main:stable) source_branch=main ;;
  *) die "Unsupported release event/ref/mode" ;;
esac
[[ "$SOURCE_REF" == "refs/heads/$source_branch" ]] || die "Source ref differs from release authority"
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
# active runs to stop operations. A pinned candidate stays valid as its branch advances.
require_release_authority() {
  local workflow_state
  workflow_state=$(gh api "repos/$GH_REPO/actions/workflows/publish-cli.yml" --jq .state) || die "Could not verify release workflow state"
  [[ "$workflow_state" == active ]] || die "Release workflow is disabled"
  [[ $(git rev-parse HEAD) == "$SOURCE_SHA" ]] || die "Checkout differs from validated source"
  git fetch --no-tags origin "$SOURCE_REF:refs/remotes/origin/$source_branch" || die "Could not refresh $SOURCE_REF"
  git merge-base --is-ancestor "$WHIP_RELEASE_BASELINE" "$SOURCE_SHA" || die "Source predates clean release baseline"
  git merge-base --is-ancestor "$SOURCE_SHA" "refs/remotes/origin/$source_branch" || die "Source is not in $SOURCE_REF history"
}
require_release_authority
node apps/desktop/scripts/release-candidate.mjs verify "$candidate"

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
