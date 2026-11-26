#!/bin/sh
# Prints the release notes for agent-loop-claude <version> (no leading "v"):
# the body of CHANGELOG.md's "## <version>" section, without its heading
# (work plan #58 D2). The release workflow passes them to GoReleaser.
#
# Fails, so nothing is published, when <version> is not Release in
# cmd/agent-loop-claude/main.go, or the section is missing or empty.
#
# Usage: scripts/release-notes.sh <version>   (run from the repository root)
set -eu

if [ $# -ne 1 ] || [ -z "$1" ]; then
	echo "usage: $0 <version>" >&2
	exit 2
fi
version=$1

release=$(sed -n 's/^const Release = "\(.*\)"$/\1/p' cmd/agent-loop-claude/main.go)
if [ "$release" != "$version" ]; then
	echo "release-notes: version $version is not Release ($release) in cmd/agent-loop-claude/main.go" >&2
	exit 1
fi

notes=$(awk -v want="## $version" '
	$0 == want { found = 1; next }
	found && /^## / { exit }
	found { print }
' CHANGELOG.md)

if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
	echo "release-notes: CHANGELOG.md has no \"## $version\" section, or it is empty" >&2
	exit 1
fi
printf '%s\n' "$notes" | sed '/./,$!d'
