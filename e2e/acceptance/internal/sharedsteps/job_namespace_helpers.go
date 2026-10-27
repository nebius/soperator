package sharedsteps

import "path"

func jobNamespacePath(jobID string) string {
	return path.Join("/tmp", jobID)
}
