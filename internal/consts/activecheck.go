package consts

const (
	ActiveCheckFinalizer                    = "slurm.nebius.ai/activecheck-finalizer"
	SlurmNodeReasonActiveCheckFailedUnknown = "Soperator activecheck unknown: node drain process"
	SlurmNodeReasonAllReducePerfNcclFailed  = "[HC] All reduce perf nccl failed"

	AllReducePerfNcclActiveCheckName = "all-reduce-perf-nccl"

	ActiveCheckEachWorkerJobArrayEnv = "EACH_WORKER_JOB_ARRAY"
	ActiveCheckNameEnv               = "ACTIVE_CHECK_NAME"
)
