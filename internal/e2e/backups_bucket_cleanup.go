package e2e

import (
	"context"
	"fmt"
	"log"
	"os/exec"
)

// backupsBucketName is the deterministic name of the e2e backups bucket,
// matching ${instance_name}-backups in soperator/modules/backups_store/main.tf
// where instance_name = k8s_cluster_name = soperator-e2e-test.
const backupsBucketName = k8sClusterName + "-backups"

// bestEffortEmptyBackupsBucket removes all objects from the e2e backups bucket.
// It is called before init-time `tf destroy` to recover from a previous run
// where terraform destroy failed with BucketNotEmpty and left the bucket
// behind. It is best-effort: any failure is logged and swallowed so init can
// proceed to `tf destroy`, which will report the real error if the bucket
// genuinely can't be deleted.
//
// The AWS CLI is pre-configured by the calling workflow step with the bucket's
// region and nebius storage endpoint, so we just shell out.
func bestEffortEmptyBackupsBucket(ctx context.Context) {
	if err := emptyBackupsBucket(ctx, backupsBucketName); err != nil {
		log.Printf("Best-effort empty of bucket %s failed: %v", backupsBucketName, err)
	}
}

func prepareBackupsBucketDelete(ctx context.Context, resource cloudResource) error {
	return emptyBackupsBucket(ctx, resource.Metadata.Name)
}

func emptyBackupsBucket(ctx context.Context, bucketName string) error {
	if _, err := exec.LookPath("aws"); err != nil {
		return fmt.Errorf("find aws CLI: %w", err)
	}

	if err := exec.CommandContext(ctx, "aws", "s3api", "head-bucket", "--bucket", bucketName).Run(); err != nil {
		log.Printf("Backups bucket %s does not exist or is not accessible, skipping object cleanup", bucketName)
		return nil
	}

	log.Printf("Emptying backups bucket %s", bucketName)
	out, err := exec.CommandContext(ctx, "aws", "s3", "rm", fmt.Sprintf("s3://%s/", bucketName), "--recursive").CombinedOutput()
	if err != nil {
		return fmt.Errorf("empty backups bucket %s: %w\nOutput: %s", bucketName, err, string(out))
	}
	log.Printf("Backups bucket %s emptied", bucketName)
	return nil
}
