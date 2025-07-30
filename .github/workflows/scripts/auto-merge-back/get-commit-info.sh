#!/bin/bash
set -euo pipefail

# Get commit details
COMMIT_SHA="${1:-${GITHUB_SHA}}"
SHORT_SHA="${COMMIT_SHA:0:7}"
COMMIT_MESSAGE="$(git log -1 --pretty=format:'%s' ${COMMIT_SHA})"
COMMIT_AUTHOR_NAME="$(git log -1 --pretty=format:'%an' ${COMMIT_SHA})"
COMMIT_AUTHOR_EMAIL="$(git log -1 --pretty=format:'%ae' ${COMMIT_SHA})"

# Extract branch name
RELEASE_BRANCH="${2:-${GITHUB_REF_NAME}}"

# Output for GitHub Actions
echo "sha=${COMMIT_SHA}" >> "${GITHUB_OUTPUT}"
echo "short_sha=${SHORT_SHA}" >> "${GITHUB_OUTPUT}"
echo "message=${COMMIT_MESSAGE}" >> "${GITHUB_OUTPUT}"
echo "author_name=${COMMIT_AUTHOR_NAME}" >> "${GITHUB_OUTPUT}"
echo "author_email=${COMMIT_AUTHOR_EMAIL}" >> "${GITHUB_OUTPUT}"
echo "release_branch=${RELEASE_BRANCH}" >> "${GITHUB_OUTPUT}"

# Log for debugging
echo "Commit SHA: ${COMMIT_SHA}"
echo "Commit Message: ${COMMIT_MESSAGE}"
echo "Author: ${COMMIT_AUTHOR_NAME} <${COMMIT_AUTHOR_EMAIL}>"
echo "Release Branch: ${RELEASE_BRANCH}"