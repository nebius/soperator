#!/bin/bash
set -euo pipefail

MERGE_PR="$1"
WORKTREE_DIR=""

cleanup() {
  if [ -n "$WORKTREE_DIR" ] && [ -d "$WORKTREE_DIR" ]; then
    echo "Cleaning up worktree..."
    git worktree remove --force "$WORKTREE_DIR"
  fi
}
trap cleanup EXIT

ORIG_PR=$(gh pr view "$MERGE_PR" --json body -q '.body' \
  | sed -n 's/.*Pull Request #\([0-9]*\).*/\1/p')
if [ -z "$ORIG_PR" ]; then
  echo "Error: could not find original PR number in PR #$MERGE_PR body"
  exit 1
fi
echo "Original PR: #$ORIG_PR"

MERGE_SHA=$(gh pr view "$ORIG_PR" --json mergeCommit -q '.mergeCommit.oid')
if [ -z "$MERGE_SHA" ]; then
  echo "Error: PR #$ORIG_PR has no merge commit (not merged yet?)"
  exit 1
fi
echo "Merge commit: ${MERGE_SHA:0:7}"

BRANCH=$(gh pr view "$MERGE_PR" --json headRefName -q '.headRefName')
echo "Branch: $BRANCH"

git fetch origin "$BRANCH"
WORKTREE_DIR=$(mktemp -d)
git worktree add "$WORKTREE_DIR" "origin/$BRANCH"

pushd "$WORKTREE_DIR" > /dev/null
git checkout -B "$BRANCH" "origin/$BRANCH"
# Merge commits need -m 1 to specify which parent to revert to
PARENT_COUNT=$(git cat-file -p "$MERGE_SHA" | grep -c '^parent ')
if [ "$PARENT_COUNT" -gt 1 ]; then
  echo "Merge commit detected, reverting with -m 1"
  git revert --no-edit -m 1 "$MERGE_SHA"
else
  git revert --no-edit "$MERGE_SHA"
fi
popd > /dev/null

git -C "$WORKTREE_DIR" push origin "$BRANCH"

gh pr comment "$MERGE_PR" \
  --body "Reverted changes from PR #$ORIG_PR (commit ${MERGE_SHA:0:7})"
echo "Done. Reverted PR #$ORIG_PR on merge-to-main PR #$MERGE_PR"
