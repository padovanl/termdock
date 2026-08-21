#!/usr/bin/env bash
# Tags and pushes a release. Usage: scripts/release.sh <version>
# <version> can be given with or without the leading "v" (e.g. 0.1.0 or v0.1.0).
#
# Releases are cut from master, which is where develop lands via pull
# request — see CONTRIBUTING-style notes in README.md's "Releasing".
# Pushing the tag is the only thing that triggers a release: the Release
# workflow builds the binaries, the .deb/.rpm/.apk packages and the
# archives, and publishes them against that tag.
set -euo pipefail

readonly RELEASE_BRANCH="master"
readonly REMOTE="origin"

if [ $# -ne 1 ]; then
	echo "Usage: $0 <version>   e.g. $0 v0.1.0" >&2
	exit 1
fi

version="$1"
[[ "$version" == v* ]] || version="v${version}"

if ! [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]; then
	echo "error: '$version' doesn't look like a valid semver tag (expected vX.Y.Z)" >&2
	exit 1
fi

branch="$(git rev-parse --abbrev-ref HEAD)"
if [ "$branch" != "$RELEASE_BRANCH" ]; then
	echo "error: releases are cut from '${RELEASE_BRANCH}' (you're on '$branch')" >&2
	echo "hint: merge develop into ${RELEASE_BRANCH} via pull request first" >&2
	exit 1
fi

if [ -n "$(git status --porcelain)" ]; then
	echo "error: working tree is not clean, commit or stash changes first" >&2
	exit 1
fi

echo "Fetching ${REMOTE}..."
git fetch "$REMOTE" --quiet --tags "$RELEASE_BRANCH"

if [ "$(git rev-parse HEAD)" != "$(git rev-parse "${REMOTE}/${RELEASE_BRANCH}")" ]; then
	ahead="$(git rev-list --count "${REMOTE}/${RELEASE_BRANCH}..HEAD")"
	behind="$(git rev-list --count "HEAD..${REMOTE}/${RELEASE_BRANCH}")"
	if [ "$behind" != "0" ]; then
		echo "error: local ${RELEASE_BRANCH} is behind ${REMOTE}/${RELEASE_BRANCH} by ${behind} commit(s); run 'git pull ${REMOTE} ${RELEASE_BRANCH}'" >&2
	else
		echo "error: local ${RELEASE_BRANCH} is ahead of ${REMOTE}/${RELEASE_BRANCH} by ${ahead} commit(s); run 'git push ${REMOTE} ${RELEASE_BRANCH}'" >&2
	fi
	exit 1
fi

if git rev-parse -q --verify "refs/tags/${version}" >/dev/null; then
	echo "error: tag '${version}' already exists locally" >&2
	exit 1
fi

if git ls-remote --exit-code --tags "$REMOTE" "refs/tags/${version}" >/dev/null 2>&1; then
	echo "error: tag '${version}' already exists on ${REMOTE}" >&2
	exit 1
fi

# The tag is what publishes a release, so it's worth knowing the tests
# pass before pushing one rather than finding out from a red workflow.
if command -v go >/dev/null 2>&1; then
	echo "Running tests..."
	if ! go test ./... >/dev/null; then
		echo "error: tests fail on ${RELEASE_BRANCH}; not tagging" >&2
		exit 1
	fi
	echo "Tests pass."
else
	echo "warning: go not found, skipping the local test run" >&2
fi

commit="$(git rev-parse --short HEAD)"
read -r -p "Create and push tag ${version} on ${commit}? [y/N] " confirm
if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
	echo "Aborted."
	exit 1
fi

git tag -a "$version" -m "termdock ${version}"
git push "$REMOTE" "$version"

echo "Pushed ${version} — https://github.com/padovanl/termdock/actions"
