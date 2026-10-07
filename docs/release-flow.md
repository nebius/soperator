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

Changes made to release branches are automatically merged forward through the chain of
release branches and eventually to `main`. The workflow detects the next release branch
by version order, so no manual configuration is needed when creating new release branches.

With a single release branch, merges go directly to `main`:

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

With multiple release branches, changes flow through a waterfall chain:

```
main                       ──●────────────────────────────────────●──▶
                             │                                    ↑
                             │                         fix-1''    │
soperator-release-3.0        ├────────────────●────────●──────────●──▶
                             │                ↑        ↑
                             │       fix-1'   │        │
soperator-release-2.0        └───────●────────●────────●─────────────▶
                                   fix-1     fix-2   bump

merge chain: release-2.0 → release-3.0 → main
```

Each push to a release branch triggers the workflow, which creates a PR targeting the
next release branch in version order. When that PR is merged, the push to the target
release branch triggers the workflow again, continuing the chain until `main` is reached.

- GitHub workflow creates merge-back PRs automatically
- PRs assigned to original commit author
- Includes original PR descriptions
- Target is determined dynamically from existing `soperator-release-*` branches

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
- Wait for automatic merge-back PR (targets the next release branch, or `main` if none)
- Ensure each merge-back PR in the chain is merged without conflicts, resolve conflicts if needed

## Release Notes

GitHub releases include the text under `## Release Notes` in each merged PR's
description, grouped into the existing changelog categories. Each entry includes
the PR title and a link to the PR, followed by its release notes.

Write user-facing release notes in the PR template, including any breaking changes
or migration steps. Markdown formatting is preserved, and HTML comments from the
template are removed. The section ends at the next level-one or level-two heading,
or at the end of the description. If the section is missing or empty, the release
entry contains only the PR title and link.

## Release Tracking Issue

To start a release:

1. Create a release tracking issue using the [Release Process Issue Template](../.github/ISSUE_TEMPLATE/release.md)
2. Fill in the target version in the required format (MAJOR.MINOR.PATCH, e.g., 1.99.0)
3. Follow the issue checklist step by step to complete the release
