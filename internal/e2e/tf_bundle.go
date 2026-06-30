package e2e

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// tfBundleS3Key is the object key, within the same bucket that holds the e2e terraform state.
// A single object is overwritten on each run.
const tfBundleS3Key = "e2e-tf-bundle/e2e-test/bundle.tar.gz"

// bundleBucket returns the S3 bucket used for the e2e terraform state.
func bundleBucket() (string, error) {
	bucket := os.Getenv("NEBIUS_BUCKET_NAME")
	if bucket == "" {
		return "", fmt.Errorf("NEBIUS_BUCKET_NAME is not set")
	}
	return bucket, nil
}

// terraformRepoRoot returns the root of the checked-out terraform repository (.../terraform-repo).
// PathToInstallation points at <repo>/soperator/installations/example, three levels below the repo root.
func terraformRepoRoot(cfg Config) string {
	return filepath.Clean(filepath.Join(cfg.PathToInstallation, "..", "..", ".."))
}

// saveBundle tars the whole terraform-repo working tree — sources, the .terraform provider/module cache,
// the lockfile, and the generated var/backend files — and uploads it to S3.
func saveBundle(ctx context.Context, cfg Config) error {
	bucket, err := bundleBucket()
	if err != nil {
		return err
	}

	repoRoot := terraformRepoRoot(cfg)
	parent := filepath.Dir(repoRoot)
	repoName := filepath.Base(repoRoot)

	archive, err := os.CreateTemp("", "e2e-tf-bundle-*.tar.gz")
	if err != nil {
		return fmt.Errorf("create temp bundle file: %w", err)
	}
	archivePath := archive.Name()
	_ = archive.Close()
	defer func() { _ = os.Remove(archivePath) }()

	tarCmd := exec.CommandContext(ctx, "tar",
		"-C", parent,
		"--exclude", filepath.Join(repoName, ".git"),
		"-czf", archivePath,
		repoName,
	)
	if out, err := tarCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tar terraform bundle: %w\nOutput: %s", err, string(out))
	}

	dst := fmt.Sprintf("s3://%s/%s", bucket, tfBundleS3Key)
	log.Printf("Uploading terraform bundle (%s) to %s", bundleArchiveSize(archivePath), dst)
	if out, err := exec.CommandContext(ctx, "aws", "s3", "cp", archivePath, dst).CombinedOutput(); err != nil {
		return fmt.Errorf("upload terraform bundle to %s: %w\nOutput: %s", dst, err, string(out))
	}
	log.Printf("Terraform bundle uploaded to %s", dst)
	return nil
}

// bundleExists reports whether a saved terraform bundle is present in S3.
// Any error (missing object, auth failure, missing CLI) is treated as "no usable bundle".
func bundleExists(ctx context.Context) bool {
	bucket, err := bundleBucket()
	if err != nil {
		log.Printf("Cannot check for terraform bundle: %v", err)
		return false
	}
	if err := exec.CommandContext(ctx, "aws", "s3api", "head-object",
		"--bucket", bucket, "--key", tfBundleS3Key).Run(); err != nil {
		log.Printf("No saved terraform bundle at s3://%s/%s", bucket, tfBundleS3Key)
		return false
	}
	return true
}

// deleteBundle removes the saved terraform bundle from S3. It is best-effort: failures are logged and swallowed.
func deleteBundle(ctx context.Context) {
	bucket, err := bundleBucket()
	if err != nil {
		log.Printf("Skipping terraform bundle delete: %v", err)
		return
	}
	dst := fmt.Sprintf("s3://%s/%s", bucket, tfBundleS3Key)
	if out, err := exec.CommandContext(ctx, "aws", "s3", "rm", dst).CombinedOutput(); err != nil {
		log.Printf("Best-effort delete of terraform bundle %s failed: %v\nOutput: %s", dst, err, string(out))
		return
	}
	log.Printf("Deleted terraform bundle %s", dst)
}

// downloadAndExtractBundle downloads the saved terraform bundle and extracts it into a fresh temp directory,
// returning the extracted installation directory (where terraform should run)
// and a cleanup func that removes the temp tree.
func downloadAndExtractBundle(ctx context.Context, cfg Config) (workdir string, cleanup func(), err error) {
	bucket, err := bundleBucket()
	if err != nil {
		return "", nil, err
	}

	tmpDir, err := os.MkdirTemp("", "e2e-tf-bundle-extract-")
	if err != nil {
		return "", nil, fmt.Errorf("create temp extract dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(tmpDir) }

	archivePath := filepath.Join(tmpDir, "bundle.tar.gz")
	src := fmt.Sprintf("s3://%s/%s", bucket, tfBundleS3Key)
	if out, err := exec.CommandContext(ctx, "aws", "s3", "cp", src, archivePath).CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("download terraform bundle from %s: %w\nOutput: %s", src, err, string(out))
	}
	log.Printf("Downloaded terraform bundle (%s) from %s", bundleArchiveSize(archivePath), src)

	if out, err := exec.CommandContext(ctx, "tar", "-C", tmpDir, "-xzf", archivePath).CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("extract terraform bundle: %w\nOutput: %s", err, string(out))
	}

	repoRoot := terraformRepoRoot(cfg)
	relInstall, err := filepath.Rel(repoRoot, cfg.PathToInstallation)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("resolve installation path relative to repo root: %w", err)
	}
	workdir = filepath.Join(tmpDir, filepath.Base(repoRoot), relInstall)
	if _, err := os.Stat(workdir); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("extracted bundle missing installation dir %s: %w", workdir, err)
	}
	return workdir, cleanup, nil
}

// bundleArchiveSize returns a human-readable size of the bundle archive,
// or "unknown size" if it can't be stat'd.
func bundleArchiveSize(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "unknown size"
	}
	return formatBytes(info.Size())
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
