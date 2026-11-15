#!/bin/bash
# Checks that every commit between two revisions is a Conventional Commit and that there are no merge commits
# (pull requests are rebased onto main). Usage: check-commits.sh <base> <head>
set -u
base=$1 head=$2
types='build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test'
pattern="^(${types})(\([a-z0-9._/-]+\))?!?: [^ ].{0,98}$"
bad=0
if [ -n "$(git rev-list --merges "$base..$head")" ]; then
  echo "::error::The branch contains merge commits. Rebase it onto main instead: git fetch origin && git rebase origin/main"
  bad=1
fi
while read -r sha; do
  [ -z "$sha" ] && continue
  subject=$(git log -1 --format=%s "$sha")
  if ! printf '%s\n' "$subject" | grep -Eq "$pattern"; then
    echo "::error::${sha:0:12}: \"$subject\" is not a Conventional Commit (type(scope): description, at most 100 characters; types: ${types//|/, })"
    bad=1
  fi
done < <(git rev-list --no-merges "$base..$head")
[ "$bad" = 0 ] && echo "All commits follow Conventional Commits."
exit "$bad"
