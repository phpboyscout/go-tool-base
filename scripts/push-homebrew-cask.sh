#!/usr/bin/env bash
# push-homebrew-cask.sh: commit the cask GoReleaser rendered into dist/ to the
# Homebrew tap. Run by the homebrew-tap job on a release tag; GoReleaser's own
# push needed an SSH deploy key (its token: field is GitHub-only), so the job
# pushes with the CI job token instead, which the tap allowlists.
#
#   TAP_URL     the tap to push to (default: the estate tap over HTTPS with the
#               job token)
#   DIST        where GoReleaser wrote its output (default: dist)
#   TAG         the release tag, for the commit message (default: CI_COMMIT_TAG)
set -euo pipefail

name=gtb
dist="${DIST:-dist}"
tag="${TAG:-${CI_COMMIT_TAG:?CI_COMMIT_TAG or TAG must be set}}"
tap_url="${TAP_URL:-https://gitlab-ci-token:${CI_JOB_TOKEN:?CI_JOB_TOKEN must be set}@gitlab.com/phpboyscout/homebrew.git}"

mapfile -t casks < <(find "$dist" -type f -path "*Casks*" -name "${name}.rb")
if [ "${#casks[@]}" -ne 1 ]; then
	echo "expected one ${name}.rb under ${dist}/**/Casks, found ${#casks[@]}: ${casks[*]:-none}" >&2
	exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

git clone --quiet --depth 1 --branch main "$tap_url" "$work/tap"
mkdir -p "$work/tap/Casks"
cp "${casks[0]}" "$work/tap/Casks/${name}.rb"

cd "$work/tap"
git config user.name goreleaserbot
git config user.email matt@phpboyscout.com

if git diff --quiet -- "Casks/${name}.rb" && ! git ls-files --others --exclude-standard | grep -q .; then
	echo "Casks/${name}.rb is already at ${tag}; nothing to push"
	exit 0
fi

git add "Casks/${name}.rb"
git commit --quiet -m "Brew cask update for ${name} version ${tag}"

# Another release may push to the tap between our clone and push; replay
# this one commit on top of it rather than fail the release.
for attempt in 1 2 3; do
	if git push --quiet origin HEAD:main; then
		echo "pushed Casks/${name}.rb for ${tag}"
		exit 0
	fi

	echo "push attempt ${attempt} was rejected; rebasing on the tap's main" >&2
	git pull --quiet --rebase origin main
done

echo "could not push Casks/${name}.rb after 3 attempts" >&2
exit 1
