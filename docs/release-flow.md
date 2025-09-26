# Release Flow

This document describes the release process for the soperator project at a high level.

## Overview

The release process enables continuous development:

- Development continues on `main` branch during releases
- Dedicated `soperator-release-MAJOR.MINOR` branches for stabilization
- Hotfix releases possible without disrupting ongoing development
- Development and release stabilization are independent

## Repository Structure

Both repositories follow the same structure:
- Development branch: `main`
- Release branches: `soperator-release-MAJOR.MINOR`

## Automatic Merge-Backs

Changes made to release branches are automatically merged back to `main` in both repositories:

```
                                  fix-1'     fix-2'           bump'
main                   ──●────────●──●─────────●───●─────────●──●──▶ (features + fixes)
                         │           ↑         ↑                ↑
                         │           │         │                │ automatic PRs
                         │           │         │                │ (assigned to author)
soperator-release-1.22   └───────────●─────────●────────────────●─▶  (stabilization)
                         │         fix-1     fix-2            bump
                         └─ branch created
```

- GitHub workflow creates merge-back PRs automatically
- PRs assigned to original commit author
- Includes original PR descriptions

## E2E Testing

E2E tests run automatically every 2 hours on:
- `main` branch
- Current release branch (`soperator-release-*`)

This ensures quality standards for both development and release branches.

## Developer Workflow

### New Features
- New features should go to `main` branch
- They will be included in the next release

### Bug Fixes
- Bug fixes can go to release branches if fixing them there is needed
- Make changes in the release branch via PR
- Wait for automatic merge-back PR to `main`
- Ensure the back-to-main PR is merged without conflicts, resolve conflicts if needed

## Release Tracking Issue

To start a release:

1. Create a release tracking issue using the [Release Process Issue Template](../.github/ISSUE_TEMPLATE/release.md)
2. Fill in the target version in the required format (MAJOR.MINOR.PATCH, e.g., 1.99.0)
3. Follow the issue checklist step by step to complete the release
