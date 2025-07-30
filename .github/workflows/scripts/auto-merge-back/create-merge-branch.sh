#!/bin/bash
set -euo pipefail

# Input parameters
PR_HEAD_REF="${1}"
RELEASE_BRANCH="${2}"
SHORT_SHA="${3}"

# Use original PR branch name if available, otherwise use release branch with SHA
if [ -n "${PR_HEAD_REF}" ]; then
    NEW_BRANCH="merge-to-main-from/${PR_HEAD_REF}"
else
    NEW_BRANCH="merge-to-main-from/${RELEASE_BRANCH}-${SHORT_SHA}"
fi

# Create and push the branch
git checkout -b "${NEW_BRANCH}"
git push origin "${NEW_BRANCH}"

# Output for GitHub Actions
echo "branch=${NEW_BRANCH}" >> "${GITHUB_OUTPUT}"
echo "Created branch: ${NEW_BRANCH}"