package slurmapi

import (
	"net/http"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
)

// Decode only fields used by Job conversion. The generated Slurm bindings use
// signed integers for unsigned values such as priority, nice and CPU frequency.
// Decoding those unused fields can reject an otherwise valid job response.
type controllerJob struct {
	JobId           *int32                      `json:"job_id"`
	Name            *string                     `json:"name"`
	JobState        *[]api.V0044JobInfoJobState `json:"job_state"`
	StateReason     *string                     `json:"state_reason"`
	Partition       *string                     `json:"partition"`
	UserId          *int64                      `json:"user_id"`
	UserName        *string                     `json:"user_name"`
	GroupName       *string                     `json:"group_name"`
	MailUser        *string                     `json:"mail_user"`
	StandardError   *string                     `json:"standard_error"`
	StandardOutput  *string                     `json:"standard_output"`
	Nodes           *string                     `json:"nodes"`
	ScheduledNodes  *string                     `json:"scheduled_nodes"`
	RequiredNodes   *string                     `json:"required_nodes"`
	NodeCount       *optionalNumber[int32]      `json:"node_count"`
	ArrayJobId      *optionalNumber[int32]      `json:"array_job_id"`
	ArrayTaskId     *optionalNumber[int32]      `json:"array_task_id"`
	ArrayTaskString *string                     `json:"array_task_string"`
	SubmitTime      *optionalNumber[int64]      `json:"submit_time"`
	StartTime       *optionalNumber[int64]      `json:"start_time"`
	EndTime         *optionalNumber[int64]      `json:"end_time"`
	TresAllocStr    *string                     `json:"tres_alloc_str"`
	TresReqStr      *string                     `json:"tres_req_str"`
	Cpus            *optionalNumber[int32]      `json:"cpus"`
	MemoryPerNode   *optionalNumber[int64]      `json:"memory_per_node"`
}

type accountingJob struct {
	JobId       *int32  `json:"job_id"`
	Name        *string `json:"name"`
	Partition   *string `json:"partition"`
	User        *string `json:"user"`
	Association *struct {
		User string `json:"user"`
	} `json:"association"`
	State *struct {
		Current *[]api.V0044JobStateCurrent `json:"current"`
		Reason  *string                     `json:"reason"`
	} `json:"state"`
	Stderr          *string `json:"stderr"`
	StderrExpanded  *string `json:"stderr_expanded"`
	Stdout          *string `json:"stdout"`
	StdoutExpanded  *string `json:"stdout_expanded"`
	Nodes           *string `json:"nodes"`
	AllocationNodes *int32  `json:"allocation_nodes"`
	Array           *struct {
		JobId  *int32                 `json:"job_id"`
		TaskId *optionalNumber[int32] `json:"task_id"`
		Task   *string                `json:"task"`
	} `json:"array"`
	Time *struct {
		Submission *int64                 `json:"submission"`
		Start      *int64                 `json:"start"`
		End        *int64                 `json:"end"`
		Limit      *optionalNumber[int32] `json:"limit"`
	} `json:"time"`
	Tres *struct {
		Allocated *api.V0044TresList `json:"allocated"`
		Requested *api.V0044TresList `json:"requested"`
	} `json:"tres"`
	Required *struct {
		CPUs          *int32                 `json:"CPUs"`
		MemoryPerNode *optionalNumber[int64] `json:"memory_per_node"`
	} `json:"required"`
}

type jobsResponse[T any] struct {
	Jobs []T `json:"jobs"`
}

func decodeJobsResponse[T any](resp *http.Response) ([]T, error) {
	result, err := decodeResponse[jobsResponse[T]](resp)
	if err != nil {
		return nil, err
	}
	return result.Jobs, nil
}
