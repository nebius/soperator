# Release Process

This document describes the release process for the soperator project, which involves two repositories:
- **[soperator](https://github.com/nebius/soperator)**: The main Kubernetes operator repository
- **[nebius-solutions-library](https://github.com/nebius/nebius-solutions-library)**: Terraform and Helm deployment configurations

## Repository Structure

### Soperator Repository
- Development branch: `dev`
- Release branch: `main`
- Release artifacts: Docker images, Helm charts, GitHub releases

### Nebius-Solutions-Library Repository
- Development branch: `release/soperator` (for soperator team)
- Release branch: `main`
- Release artifacts: Terraform modules, Helm chart configurations

## Branch Stabilization Process

### Stabilization Prerequisites

Before creating a release, both repositories must undergo a stabilization process to ensure quality and reliability.

### Feature Freeze

During the stabilization period, new feature development is frozen:
- No new features should be merged to development branches (`dev` in soperator, `release/soperator` in nebius-solutions-library)
- Focus shifts entirely to testing, bug fixes, and stabilization
- Only critical bug fixes and stabilization-related changes are allowed

### E2E Testing

Before creating a release, ensure that end-to-end (e2e) tests are passing. The e2e tests validate the complete deployment of soperator using terraform.

Automatic E2E Testing:
- E2E tests run automatically every hour on all active branches
- Tests deploy a full soperator cluster using terraform and validate functionality
- Test results are available in the [GitHub Actions tab](https://github.com/nebius/soperator/actions/workflows/e2e_test.yml)

Branch Combinations for E2E Tests:
- Soperator repository: Uses the current branch (e.g., `dev`)
- Nebius-solutions-library repository: Uses `release/soperator` branch by default

Manual E2E Test Trigger (if needed):
- Go to [E2E test workflow](https://github.com/nebius/soperator/actions/workflows/e2e_test.yml)
- Click "Run workflow"
- Select the branch to test (usually `dev`)
- Optionally override terraform repository settings

## Soperator Release Checklist

### Standard Release Process

1. Create version bump branch from dev
   ```bash
   git checkout dev
   git pull origin dev
   git checkout -b bump-version-to-1.22.0  # or any preferred naming
   ```

2. Update VERSION file
   ```bash
   # For minor releases: major.++minor.0
   # For major releases: ++major.0.0
   echo "1.22.0" > VERSION
   ```

3. Synchronize versions across the codebase
   ```bash
   make sync-version-from-scratch
   ```
   This command updates:
   - `internal/consts/version.go`
   - Helm chart versions and app versions
   - Kubernetes manifest image tags
   - Flux CD configurations
   - Docker image references

4. Create and merge PR to dev
   ```bash
   git add .
   git commit -m "bump 1.22.0"
   git push origin bump-version-to-1.22.0
   # Create PR: bump-version-to-1.22.0 → dev
   ```

   Note: Version bump PRs typically contain only version-related changes. Feature development happens in separate PRs that are merged to `dev` before the version bump.

5. Create and merge PR from dev to main
   ```bash
   # After dev PR is merged, create PR: dev → main
   ```

### Automated release creation
   
   When code is merged to `main`, GitHub Actions automatically:
   - Creates a git tag using the VERSION file content (e.g., `1.22.0`)
   - Analyzes all PRs merged from `dev` branch between the last release tag and the new tag
   - PRs are automatically categorized based on their GitHub labels into sections like Features, Fixes, Tests, Dependencies, Docs, etc.
   - PRs with `ignore-for-release` label are excluded from release notes
   - Automatically lists all contributors who authored the included PRs
   - Creates a GitHub release with generated changelog and release notes
   - Builds and publishes Docker images
   
   Important: Use descriptive PR titles as they appear directly in release notes


## Nebius-Solutions-Library Release Process

### Standard Release Process

1. Ensure main is merged to release/soperator
   ```bash
   git checkout release/soperator
   git pull origin main
   ```

2. Create version update branch from release/soperator
   ```bash
   git checkout -b update-soperator-1.22.0  # or any preferred naming
   ```

3. Update VERSION, SUBVERSION, and terraform variables
   ```bash
   # VERSION should correspond to soperator version
   echo "1.22.0" > VERSION
   
   # SUBVERSION: increment for terraform-only patches, otherwise set to 1
   echo "1" > SUBVERSION
   
   # Update slurm_operator_version in terraform.tfvars
   sed -i 's/slurm_operator_version = "[^"]*"/slurm_operator_version = "1.22.0"/' soperator/installations/example/terraform.tfvars
   ```

4. Create and merge PR to release/soperator
   ```bash
   git add .
   git commit -m "Update for soperator 1.22.0"
   git push origin update-soperator-1.22.0
   # Create PR: update-soperator-1.22.0 → release/soperator
   ```

   Note: Similar to soperator, version update PRs typically contain only version-related changes. Terraform and Helm configuration changes happen in separate PRs merged before the version update.

5. Create and merge PR from release/soperator to main

### Automated release creation
   
   When soperator-related code is merged to `main`, GitHub Actions automatically:
   - Creates git tags using both VERSION and SUBVERSION files (e.g., `soperator-v1.22.0-0`)
   - Builds and uploads terraform tarballs as release assets  
   - Creates GitHub release with naming pattern: "Soperator Terraform recipe v{VERSION}-{SUBVERSION}"




### Dependencies
- Both repositories should be released in coordination
- Terraform changes often depend on specific soperator versions
- Update VERSION in nebius-solutions-library to match soperator releases