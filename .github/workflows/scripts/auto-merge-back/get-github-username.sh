#!/bin/bash
set -euo pipefail

# Input parameters
AUTHOR_EMAIL="${1}"
GITHUB_REPOSITORY="${2:-${GITHUB_REPOSITORY}}"
GITHUB_ACTOR="${3:-${GITHUB_ACTOR}}"

# Try to get GitHub username from commit author email
echo "Searching for GitHub user with email: ${AUTHOR_EMAIL}"

USERNAME=""

# Try to extract username from noreply email format
if [[ "${AUTHOR_EMAIL}" =~ ^[0-9]+\+(.+)@users\.noreply\.github\.com$ ]]; then
    USERNAME="${BASH_REMATCH[1]}"
    echo "Extracted username from noreply email: ${USERNAME}"
else
    # Try to get username from recent commits by this author in the repo
    echo "Checking recent commits for author username..."
    USERNAME=$(gh api "repos/${GITHUB_REPOSITORY}/commits?author=${AUTHOR_EMAIL}" \
        --jq '.[0].author.login' 2>/dev/null || echo "")
fi

# Fallback to workflow actor if username not found
if [ -z "${USERNAME}" ]; then
    USERNAME="${GITHUB_ACTOR}"
    echo "Could not determine GitHub username from email, using workflow actor: ${USERNAME}"
else
    echo "Found GitHub username: ${USERNAME}"
fi

# Output for GitHub Actions
echo "username=${USERNAME}" >> "${GITHUB_OUTPUT}"