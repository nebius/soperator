package e2e

import "testing"

func TestTerraformRepoRoot(t *testing.T) {
	cfg := Config{PathToInstallation: "/ws/terraform-repo/soperator/installations/example"}
	got := terraformRepoRoot(cfg)
	const want = "/ws/terraform-repo"
	if got != want {
		t.Fatalf("terraformRepoRoot = %q, want %q", got, want)
	}
}

func TestBundleBucket(t *testing.T) {
	t.Setenv("NEBIUS_BUCKET_NAME", "tfstate-slurm-k8s-abc123")
	got, err := bundleBucket()
	if err != nil {
		t.Fatalf("bundleBucket returned error: %v", err)
	}
	if got != "tfstate-slurm-k8s-abc123" {
		t.Fatalf("bundleBucket = %q", got)
	}
}

func TestBundleBucketUnset(t *testing.T) {
	t.Setenv("NEBIUS_BUCKET_NAME", "")
	if _, err := bundleBucket(); err == nil {
		t.Fatal("expected error when NEBIUS_BUCKET_NAME is unset")
	}
}

func TestTFBundleS3Key(t *testing.T) {
	const want = "e2e-tf-bundle/e2e-test/bundle.tar.gz"
	if tfBundleS3Key != want {
		t.Fatalf("tfBundleS3Key = %q, want %q", tfBundleS3Key, want)
	}
}
