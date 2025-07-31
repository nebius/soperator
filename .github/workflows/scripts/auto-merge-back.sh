#!/bin/bash
set -euo pipefail

# ============================================
# Auto Merge-Back Workflow Script
# ============================================
# This script automatically creates pull requests to merge changes
# from release branches back to the main branch.
#
# It performs the following steps:
# 1. Extracts commit information
# 2. Finds the GitHub username for the commit author
# 3. Checks if the commit came from a PR to get original details
# 4. Creates a new branch for the merge-back
# 5. Creates a pull request to main
# ============================================

# Global variables
declare COMMIT_SHA
declare SHORT_SHA
declare COMMIT_MESSAGE
declare COMMIT_AUTHOR_NAME
declare COMMIT_AUTHOR_EMAIL
declare RELEASE_BRANCH
declare USERNAME
declare PR_NUMBER
declare PR_TITLE
declare PR_HEAD_REF
declare NEW_BRANCH

# ============================================
# FUNCTION: Get commit information
# ============================================
get_commit_info() {
    echo "=== Getting commit information ==="
    
    # Get commit details
    # TEMPORARY: Hardcode test commit
    COMMIT_SHA="36f3084390c48f3a42fcc2ccb2a883bd0a0c172d"  # PR #1319: Fix bump version detection
    # COMMIT_SHA="${GITHUB_SHA}"
    SHORT_SHA="${COMMIT_SHA:0:7}"
    COMMIT_MESSAGE="$(git log -1 --pretty=format:'%s' ${COMMIT_SHA})"
    COMMIT_AUTHOR_NAME="$(git log -1 --pretty=format:'%an' ${COMMIT_SHA})"
    COMMIT_AUTHOR_EMAIL="$(git log -1 --pretty=format:'%ae' ${COMMIT_SHA})"
    # TEMPORARY: Hardcode release branch for testing
    RELEASE_BRANCH="soperator-release-1.21"
    # RELEASE_BRANCH="${GITHUB_REF_NAME}"
    
    # Log for debugging
    echo "Commit SHA: ${COMMIT_SHA}"
    echo "Commit Message: ${COMMIT_MESSAGE}"
    echo "Author: ${COMMIT_AUTHOR_NAME} <${COMMIT_AUTHOR_EMAIL}>"
    echo "Release Branch: ${RELEASE_BRANCH}"
}

# ============================================
# FUNCTION: Get GitHub username from email
# ============================================
get_github_username() {
    echo "=== Finding GitHub username ==="
    echo "Searching for GitHub user with email: ${COMMIT_AUTHOR_EMAIL}"
    
    USERNAME=""
    
    # Try to extract username from noreply email format
    if [[ "${COMMIT_AUTHOR_EMAIL}" =~ ^[0-9]+\+(.+)@users\.noreply\.github\.com$ ]]; then
        USERNAME="${BASH_REMATCH[1]}"
        echo "Extracted username from noreply email: ${USERNAME}"
    else
        # Try to get username from recent commits by this author in the repo
        echo "Checking recent commits for author username..."
        USERNAME=$(gh api "repos/${GITHUB_REPOSITORY}/commits?author=${COMMIT_AUTHOR_EMAIL}" \
            --jq '.[0].author.login' 2>/dev/null || echo "")
    fi
    
    # Fallback to workflow actor if username not found
    if [ -z "${USERNAME}" ]; then
        USERNAME="${GITHUB_ACTOR}"
        echo "Could not determine GitHub username from email, using workflow actor: ${USERNAME}"
    else
        echo "Found GitHub username: ${USERNAME}"
    fi
}

# ============================================
# FUNCTION: Get PR information
# ============================================
get_pr_info() {
    echo "=== Checking for associated pull request ==="
    
    # Search for PRs that were merged into this release branch and contain this commit
    local pr_info=$(gh pr list --state merged --base "${RELEASE_BRANCH}" \
        --json number,title,mergeCommit,headRefName \
        --jq ".[] | select(.mergeCommit.oid == \"${COMMIT_SHA}\") | {number, title, headRefName}" \
        2>/dev/null || echo "")
    
    if [ -n "${pr_info}" ]; then
        PR_NUMBER=$(echo "${pr_info}" | jq -r '.number')
        PR_TITLE=$(echo "${pr_info}" | jq -r '.title')
        PR_HEAD_REF=$(echo "${pr_info}" | jq -r '.headRefName')
        echo "Found PR #${PR_NUMBER}: ${PR_TITLE}"
        echo "Original branch: ${PR_HEAD_REF}"
    else
        echo "No associated PR found for this commit"
        PR_NUMBER=""
        PR_TITLE=""
        PR_HEAD_REF=""
    fi
}

# ============================================
# FUNCTION: Create merge-back branch
# ============================================
create_merge_branch() {
    echo "=== Creating merge-back branch ==="
    
    # Use original PR branch name if available, otherwise use release branch with SHA
    if [ -n "${PR_HEAD_REF}" ]; then
        NEW_BRANCH="merge-to-main-from/${PR_HEAD_REF}"
    else
        NEW_BRANCH="merge-to-main-from/${RELEASE_BRANCH}-${SHORT_SHA}"
    fi
    
    # Create and push the branch
    git checkout -b "${NEW_BRANCH}"
    git push origin "${NEW_BRANCH}"
    
    echo "Created branch: ${NEW_BRANCH}"
}

# ============================================
# FUNCTION: Create pull request
# ============================================
create_pull_request() {
    echo "=== Creating pull request ==="
    
    # Determine PR title
    local pr_title
    if [ -n "${PR_TITLE}" ]; then
        pr_title="Merge to main: ${PR_TITLE}"
    else
        pr_title="Merge to main: ${COMMIT_MESSAGE}"
    fi
    echo "PR Title: ${pr_title}"
    
    # Build PR body
    local pr_body="## Merge back from release branch

This PR merges changes from the release branch back to the main branch.

### Source Information
- **Source branch**: \`${RELEASE_BRANCH}\`
- **Target branch**: \`main\`
- **Commit**: ${COMMIT_SHA}
- **Author**: ${COMMIT_AUTHOR_NAME} <${COMMIT_AUTHOR_EMAIL}>
- **GitHub user**: @${USERNAME}"
    
    if [ -n "${PR_NUMBER}" ]; then
        pr_body="${pr_body}
- **Original PR**: #${PR_NUMBER}"
    fi
    
    pr_body="${pr_body}

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
        --title "${pr_title}" \
        --body "${pr_body}" \
        --assignee "${USERNAME}" \
        --label "ignore-for-release"
    
    echo "Pull request created successfully"
}

# ============================================
# MAIN EXECUTION
# ============================================
main() {
    echo "========================================"
    echo "Auto Merge-Back Workflow"
    echo "========================================"
    
    # Execute all steps
    get_commit_info
    get_github_username
    get_pr_info
    create_merge_branch
    create_pull_request
    
    echo "========================================"
    echo "Workflow completed successfully"
    echo "========================================"
}

# Run main function
main "$@"