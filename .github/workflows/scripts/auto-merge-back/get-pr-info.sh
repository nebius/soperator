#!/bin/bash
set -euo pipefail

# Input parameters
COMMIT_SHA="${1}"
RELEASE_BRANCH="${2}"

# Search for PRs that were merged into this release branch and contain this commit
PR_INFO=$(gh pr list --state merged --base "${RELEASE_BRANCH}" --json number,title,mergeCommit,headRefName \
    --jq ".[] | select(.mergeCommit.oid == \"${COMMIT_SHA}\") | {number, title, headRefName}" \
    2>/dev/null || echo "")

if [ -n "${PR_INFO}" ]; then
    PR_NUMBER=$(echo "${PR_INFO}" | jq -r '.number')
    PR_TITLE=$(echo "${PR_INFO}" | jq -r '.title')
    PR_HEAD_REF=$(echo "${PR_INFO}" | jq -r '.headRefName')
    echo "Found PR #${PR_NUMBER}: ${PR_TITLE}"
    echo "Original branch: ${PR_HEAD_REF}"
    echo "pr_number=${PR_NUMBER}" >> "${GITHUB_OUTPUT}"
    echo "pr_title=${PR_TITLE}" >> "${GITHUB_OUTPUT}"
    echo "pr_head_ref=${PR_HEAD_REF}" >> "${GITHUB_OUTPUT}"
else
    echo "No associated PR found for this commit"
    echo "pr_number=" >> "${GITHUB_OUTPUT}"
    echo "pr_title=" >> "${GITHUB_OUTPUT}"
    echo "pr_head_ref=" >> "${GITHUB_OUTPUT}"
fi