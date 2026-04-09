package consts

const (
	ActiveCheckFinalizer = K8sGroupNameSoperator + "/activecheck-finalizer"

	ActiveCheckEachWorkerJobsEnv  = "EACH_WORKER_JOBS"
	ActiveCheckNameEnv            = "ACTIVE_CHECK_NAME"
	ActiveCheckMaxNumberOfJobsEnv = "ACTIVE_CHECK_MAX_NUMBER_OF_JOBS"
	ActiveCheckRequiresGPUEnv     = "ACTIVE_CHECK_REQUIRES_GPU"

	ActiveCheckSkippedReasonAnnotation = "slurm-skipped-reason"
)
