#!/bin/bash
set -euo pipefail

# Input parameters
PR_TITLE_FROM_COMMIT="${1}"
COMMIT_MESSAGE="${2}"
RELEASE_BRANCH="${3}"
COMMIT_SHA="${4}"
AUTHOR_NAME="${5}"
AUTHOR_EMAIL="${6}"
USERNAME="${7}"
PR_NUMBER="${8}"
NEW_BRANCH="${9}"

# Determine PR title
if [ -n "${PR_TITLE_FROM_COMMIT}" ]; then
    PR_TITLE="Merge to main: ${PR_TITLE_FROM_COMMIT}"
else
    PR_TITLE="Merge to main: ${COMMIT_MESSAGE}"
fi

# Create PR body
PR_BODY="## Merge back from release branch

This PR merges changes from the release branch back to the main branch.

### Source Information
- **Source branch**: \`${RELEASE_BRANCH}\`
- **Target branch**: \`main\`
- **Commit**: ${COMMIT_SHA}
- **Author**: ${AUTHOR_NAME} <${AUTHOR_EMAIL}>
- **GitHub user**: @${USERNAME}"

if [ -n "${PR_NUMBER}" ]; then
    PR_BODY="${PR_BODY}
- **Original PR**: #${PR_NUMBER}"
fi

PR_BODY="${PR_BODY}

### Commit Message
\`\`\`
${COMMIT_MESSAGE}
\`\`\`

---
*This PR was automatically created by the merge-back workflow.*"

# Create the PR
gh pr create \
    --base "main" \
    --head "${NEW_BRANCH}" \
    --title "${PR_TITLE}" \
    --body "${PR_BODY}" \
    --assignee "${USERNAME}" \
    --label "ignore-for-release"

echo "Pull request created successfully"