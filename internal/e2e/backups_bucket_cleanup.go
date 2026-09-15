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
	if _, err := exec.LookPath("aws"); err != nil {
		log.Printf("aws CLI not found, skipping pre-init backups bucket cleanup: %v", err)
		return
	}

	if err := exec.CommandContext(ctx, "aws", "s3api", "head-bucket", "--bucket", backupsBucketName).Run(); err != nil {
		log.Printf("Backups bucket %s does not exist or is not accessible, skipping pre-init cleanup", backupsBucketName)
		return
	}

	log.Printf("Emptying backups bucket %s before init-time destroy", backupsBucketName)
	out, err := exec.CommandContext(ctx, "aws", "s3", "rm", fmt.Sprintf("s3://%s/", backupsBucketName), "--recursive").CombinedOutput()
	if err != nil {
		log.Printf("Best-effort empty of bucket %s failed: %v\nOutput: %s", backupsBucketName, err, string(out))
		return
	}
	log.Printf("Backups bucket %s emptied", backupsBucketName)
}
