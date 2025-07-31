---
name: Release Process
about: Template for tracking soperator release process
title: 'Release soperator [VERSION]'
labels: ['release']
assignees: ''
body:
  - type: input
    id: version
    attributes:
      label: Version
      description: Target release version (format MAJOR.MINOR.PATCH, e.g. 1.99.0)
      placeholder: "1.99.0"
    validations:
      required: true
---

## Release Information

**Target Version:** [e.g., 1.99.0]
**Release Branch:** `soperator-release-[MAJOR.MINOR]` [e.g., soperator-release-1.99]

## Pre-Release Checklist (skip for hotfixes)

### Planning
- [ ] Verify that previous release has no outstanding changes
  - [ ] Try to merge release branch back to `main`
  - [ ] Ensure that there is no changes, or commit them probably by fixing merge conflicts
- [ ] Majority of planned features have been completed and merged to `main`
- [ ] Ready to start stabilization phase
- [ ] E2E tests are passing on `main` branch

### Create Release Branches
- [ ] Soperator Repository: Create `soperator-release-[MAJOR.MINOR]` branch from `main`
- [ ] Nebius-Solutions-Library Repository: Create `soperator-release-[MAJOR.MINOR]` branch from `main`
- [ ] Make an announcement to the Soperator dev team
- [ ] Update RELEASE_BRANCH variable in `.github/workflows/e2e_test_scheduler.yml`, so that E2E tests will run on the release branch

## Stabilization Phase

### Testing and Bug Fixes
- [ ] Review E2E test results on release branches
- [ ] Manual testing of major features has been completed
- [ ] All critical bugs have been identified and reported
- [ ] Bug fixes have been applied to release branches
- [ ] Verify automatic merge-back PRs are working (soperator repository)
- [ ] No long-standing merge conflicts in merge-back PRs

## Release Execution

### Soperator Repository
- [ ] Update `VERSION` file to target version, run `make sync-version-from-scratch`
- [ ] Create and merge PR to release branch
- [ ] Verify GitHub release was created automatically
- [ ] Verify that stable builds completed successfully in GitHub Actions
- [ ] Verify merge-back PR to `main` was created automatically
- [ ] Resolve any conflicts in merge-back PR and ensure it's merged

### Nebius-Solutions-Library Repository
- [ ] Update `VERSION` and/or `SUBVERSION` files to match soperator version (default SUBVERSION is 1)
- [ ] Update `slurm_operator_version` variable in `soperator/installations/example/terraform.tfvars`
- [ ] Create and merge PR to the release branch
- [ ] Verify GitHub release was created automatically
- [ ] Manually merge release branch back to `main`
- [ ] Resolve any merge conflicts during manual merge

## Post-Release Actions

### Release Validation
- [ ] Both GitHub releases are published and accessible
- [ ] Release artifacts (Docker images, Helm charts, Terraform tarballs) are available
- [ ] Release notes are accurate and complete
- [ ] All merge-back operations have completed successfully

### Communication
- [ ] Release announcement prepared
- [ ] Team notified of successful release

### Process Improvement
- [ ] Document process improvements and create issues for identified flaws

## Notes

Add any additional notes, issues encountered, or special considerations for this release:

<!--
- Any deviations from standard process
- Special testing requirements
- Known issues or limitations
- Communication requirements
-->

## Release Process Improvements

Document any flaws, inefficiencies, or improvement opportunities discovered during this release:

### Workflow Issues
<!--
- Steps that were unclear or missing
- Manual processes that could be automated
- Dependencies that caused delays
-->

### Documentation Gaps
<!--
- Missing instructions or unclear steps
- Outdated information
- Additional context needed
-->

### Automation Opportunities
<!--
- Manual steps that could be automated
- Checks that could be added to CI/CD
- Notification improvements
-->

### Follow-up Actions
<!--
Create GitHub issues for improvements identified above:
- [ ] Issue #XXX: [Brief description]
- [ ] Issue #XXX: [Brief description]
-->
