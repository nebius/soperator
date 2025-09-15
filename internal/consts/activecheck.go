package consts

const (
	ActiveCheckFinalizer                    = "slurm.nebius.ai/activecheck-finalizer"
	SlurmNodeReasonActiveCheckFailedUnknown = "Soperator activecheck unknown: node drain process"

	ActiveCheckEachWorkerJobArrayEnv = "EACH_WORKER_JOB_ARRAY"
	ActiveCheckEachWorkerJobsEnv     = "EACH_WORKER_JOBS"
	ActiveCheckNameEnv               = "ACTIVE_CHECK_NAME"
	ActiveCheckMaxNumberOfJobsEnv    = "ACTIVE_CHECK_MAX_NUMBER_OF_JOBS"
)
