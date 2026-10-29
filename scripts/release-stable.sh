#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
tag=${1:?usage: release-stable.sh vX.Y.Z}
[[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
 echo 'A stable vX.Y.Z tag is required.' >&2; exit 2
}
repo=Obedience-Corp/obey-go-sdk
[ -z "$(git status --porcelain)" ] || { echo 'Release requires a clean checkout.' >&2; exit 1; }
case "$(git remote get-url origin)" in
 git@github.com:Obedience-Corp/obey-go-sdk.git|https://github.com/Obedience-Corp/obey-go-sdk.git|https://github.com/Obedience-Corp/obey-go-sdk) ;;
 *) echo 'Release origin must be the canonical SDK repository.' >&2; exit 1 ;;
esac
git fetch origin main --tags
revision=$(git rev-parse HEAD)
[ "$revision" = "$(git rev-parse origin/main)" ] || { echo 'Release checkout must equal origin/main.' >&2; exit 1; }
if git rev-parse --verify "refs/tags/$tag" >/dev/null 2>&1; then
 echo "Tag $tag already exists; existing versions are immutable." >&2; exit 1
fi
[ "$(gh api "repos/$repo" --jq .permissions.push)" = true ] || { echo 'SDK push permission is required.' >&2; exit 1; }
ci=$(gh run list --repo "$repo" --workflow ci.yml --branch main --commit "$revision" --limit 1 \
 --json status,conclusion --jq '.[0] | [.status, .conclusion] | join(" ")')
[ "$ci" = 'completed success' ] || { echo "SDK integration on this main commit must pass first (current: $ci)." >&2; exit 1; }
bash scripts/release-check.sh
git tag -a "$tag" -m "Obey Go SDK $tag"
git push origin "$tag"
printf 'Pushed %s at %s. The release workflow publishes notes and verifies the public Go module.\n' "$tag" "$revision"
