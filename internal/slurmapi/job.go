package slurmapi

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Job struct {
	ID             int32
	Name           string
	State          string
	StateReason    string
	Partition      string
	UserName       string
	UserID         *int32
	UserMail       string
	StandardError  string
	StandardOutput string
	Nodes          string
	ScheduledNodes string
	RequiredNodes  string
	NodeCount      *int32
	ArrayJobID     *int32
	ArrayTaskID    *int32
	// ArrayTaskString is the range expression for an array job's master record (e.g. "1-5",
	// "1,3,5", "1-10:2"). Slurmdbd collapses pending array tasks into one row with this field set
	// and ArrayTaskID unset; slurmctld also populates it on the master record. Per-task records
	// have ArrayTaskID set instead. Used as a fallback when ArrayTaskID is nil so dashboards can
	// still distinguish array master records from non-array jobs.
	ArrayTaskString string
	SubmitTime      *metav1.Time
	StartTime       *metav1.Time
	EndTime         *metav1.Time
	TresAllocated   string
	TresRequested   string
	CPUs            *int32
	MemoryPerNode   *int64 // in MB
}

func JobFromAPI(apiJob api.V0041JobInfo) (Job, error) {
	job := Job{}
	if apiJob.JobId == nil {
		return job, fmt.Errorf("job ID is missing")
	}
	job.ID = *apiJob.JobId

	if apiJob.Name != nil {
		job.Name = *apiJob.Name
	}

	if apiJob.JobState == nil || len(*apiJob.JobState) == 0 {
		return Job{}, fmt.Errorf("job state is missing")
	}

	job.State = string((*apiJob.JobState)[0])

	if apiJob.StateReason != nil {
		job.StateReason = *apiJob.StateReason
	}

	if apiJob.Partition != nil {
		job.Partition = *apiJob.Partition
	}

	if apiJob.UserId != nil {
		job.UserID = apiJob.UserId
	}

	if apiJob.UserName != nil {
		job.UserName = *apiJob.UserName
	} else if apiJob.GroupName != nil {
		job.UserName = *apiJob.GroupName
	}

	// Slurm API returns mail_user = user_name if mail_user wasn't set explicitly
	// There are also instances when mail_user = user_name value, but user_name is empty
	if apiJob.MailUser != nil && job.UserName != "" && job.UserName != *apiJob.MailUser {
		job.UserMail = *apiJob.MailUser
	}

	if apiJob.StandardError != nil {
		job.StandardError = *apiJob.StandardError
	}

	if apiJob.StandardOutput != nil {
		job.StandardOutput = *apiJob.StandardOutput
	}

	if apiJob.Nodes != nil && !isUnallocatedNodeList(*apiJob.Nodes) {
		job.Nodes = *apiJob.Nodes
	}

	if apiJob.ScheduledNodes != nil {
		job.ScheduledNodes = *apiJob.ScheduledNodes
	}

	if apiJob.RequiredNodes != nil {
		job.RequiredNodes = *apiJob.RequiredNodes
	}

	job.NodeCount = convertToInt(apiJob.NodeCount)
	job.ArrayJobID = convertToInt(apiJob.ArrayJobId)
	job.ArrayTaskID = convertToInt(apiJob.ArrayTaskId)
	if apiJob.ArrayTaskString != nil {
		job.ArrayTaskString = *apiJob.ArrayTaskString
	}
	job.SubmitTime = convertToMetav1Time(apiJob.SubmitTime)
	job.StartTime = convertToMetav1Time(apiJob.StartTime)
	job.EndTime = convertToMetav1Time(apiJob.EndTime)

	if apiJob.TresAllocStr != nil {
		job.TresAllocated = *apiJob.TresAllocStr
	}
	if apiJob.TresReqStr != nil {
		job.TresRequested = *apiJob.TresReqStr
	}
	job.CPUs = convertToInt(apiJob.Cpus)
	job.MemoryPerNode = convertToInt64(apiJob.MemoryPerNode)

	return job, nil
}

func JobFromAccountingAPI(apiJob api.V0041Job) (Job, error) {
	job := Job{}
	if apiJob.JobId == nil {
		return job, fmt.Errorf("job ID is missing")
	}
	job.ID = *apiJob.JobId

	if apiJob.Name != nil {
		job.Name = *apiJob.Name
	}

	if apiJob.State == nil || apiJob.State.Current == nil || len(*apiJob.State.Current) == 0 {
		return Job{}, fmt.Errorf("job state is missing")
	}

	job.State = string((*apiJob.State.Current)[0])

	if apiJob.State.Reason != nil {
		job.StateReason = *apiJob.State.Reason
	}

	if apiJob.Partition != nil {
		job.Partition = *apiJob.Partition
	}

	if apiJob.User != nil {
		job.UserName = *apiJob.User
	} else if apiJob.Association != nil {
		job.UserName = apiJob.Association.User
	}

	if apiJob.StderrExpanded != nil {
		job.StandardError = *apiJob.StderrExpanded
	} else if apiJob.Stderr != nil {
		job.StandardError = *apiJob.Stderr
	}

	if apiJob.StdoutExpanded != nil {
		job.StandardOutput = *apiJob.StdoutExpanded
	} else if apiJob.Stdout != nil {
		job.StandardOutput = *apiJob.Stdout
	}

	if apiJob.Nodes != nil && !isUnallocatedNodeList(*apiJob.Nodes) {
		job.Nodes = *apiJob.Nodes
	}

	// V0041Job (accounting) only carries allocation_nodes — the count of nodes already assigned.
	// For pending jobs that's 0, which would zero out per-node memory metrics downstream
	// (jobAllocatedResources multiplies MemoryPerNode by NodeCount). Leave NodeCount nil in that
	// case so the fallback nodeCount=1 kicks in. The accounting API doesn't expose the originally
	// requested node count, so multi-node pending jobs will report MemoryPerNode×1 — controller
	// mode is the source of truth for live pending state.
	if apiJob.AllocationNodes != nil && *apiJob.AllocationNodes > 0 {
		job.NodeCount = apiJob.AllocationNodes
	}
	if apiJob.Array != nil {
		job.ArrayJobID = apiJob.Array.JobId
		job.ArrayTaskID = convertToInt(apiJob.Array.TaskId)
		if apiJob.Array.Task != nil {
			job.ArrayTaskString = *apiJob.Array.Task
		}
	}

	if apiJob.Time != nil {
		job.SubmitTime = unixTimeToMetav1Time(apiJob.Time.Submission)
		job.StartTime = unixTimeToMetav1Time(apiJob.Time.Start)
		job.EndTime = unixTimeToMetav1Time(apiJob.Time.End)

		// Slurmdbd stores time_end=0 for unfinished jobs. Project end = start + time_limit for
		// RUNNING jobs to mirror slurmctld's behaviour on the controller path; this keeps
		// slurm_job_info.end_time semantically consistent across sources. Time.Limit is in minutes;
		// if the job has no time limit (UNLIMITED / NO_VAL) convertToInt returns nil and we leave
		// end_time empty, which is also what the controller does.
		if job.EndTime == nil &&
			job.State == string(api.V0041JobStateCurrentRUNNING) &&
			job.StartTime != nil && job.StartTime.Unix() > 0 {
			if limitMin := convertToInt(apiJob.Time.Limit); limitMin != nil {
				projected := metav1.NewTime(job.StartTime.Add(time.Duration(*limitMin) * time.Minute))
				job.EndTime = &projected
			}
		}
	}

	if apiJob.Tres != nil {
		job.TresAllocated = tresListToString(apiJob.Tres.Allocated)
		job.TresRequested = tresListToString(apiJob.Tres.Requested)
	}

	if apiJob.Required != nil {
		job.CPUs = apiJob.Required.CPUs
		job.MemoryPerNode = convertToInt64(apiJob.Required.MemoryPerNode)
	}

	return job, nil
}

func (j Job) String() string {
	return fmt.Sprintf("Job{ID: %d, Name: %s, State: %s, StateReason: %s, Partition: %s, User: %s}",
		j.ID, j.Name, j.State, j.StateReason, j.Partition, j.UserName)
}

func (j Job) GetIDString() string {
	return strconv.Itoa(int(j.ID))
}

func (j Job) GetArrayJobIDString() string {
	if j.ArrayJobID == nil {
		return ""
	}
	return strconv.Itoa(int(*j.ArrayJobID))
}

// GetArrayTaskIDString returns the per-task array index (e.g. "3") for an exploded array task,
// falling back to the range expression (e.g. "1-5") on a master record where ArrayTaskID is unset.
// Returns "" for non-array jobs.
func (j Job) GetArrayTaskIDString() string {
	if j.ArrayTaskID != nil {
		return strconv.Itoa(int(*j.ArrayTaskID))
	}
	return j.ArrayTaskString
}

func (j Job) GetNodeList() ([]string, error) {
	return parseNodeList(j.Nodes)
}

func (j Job) GetScheduledNodeList() ([]string, error) {
	return parseNodeList(j.ScheduledNodes)
}

func (j Job) GetRequiredNodeList() ([]string, error) {
	return parseNodeList(j.RequiredNodes)
}

func (j Job) IsTerminalState() bool {
	switch j.State {
	case string(api.V0041JobInfoJobStateFAILED),
		string(api.V0041JobInfoJobStateCANCELLED),
		string(api.V0041JobInfoJobStateTIMEOUT),
		string(api.V0041JobInfoJobStateOUTOFMEMORY),
		string(api.V0041JobInfoJobStateBOOTFAIL),
		string(api.V0041JobInfoJobStateDEADLINE),
		string(api.V0041JobInfoJobStateLAUNCHFAILED),
		string(api.V0041JobInfoJobStateNODEFAIL),
		string(api.V0041JobInfoJobStatePREEMPTED),
		string(api.V0041JobInfoJobStateRECONFIGFAIL),
		string(api.V0041JobInfoJobStateREVOKED),
		string(api.V0041JobInfoJobStateSPECIALEXIT),
		string(api.V0041JobInfoJobStateCOMPLETED),
		string(api.V0041JobInfoJobStateSTOPPED):
		return true
	default:
		return false
	}
}

func (j Job) IsFailedState() bool {
	return j.State == string(api.V0041JobInfoJobStateFAILED)
}

func (j Job) IsCompletedState() bool {
	return j.State == string(api.V0041JobInfoJobStateCOMPLETED)
}

func (j Job) IsCancelledState() bool {
	return j.State == string(api.V0041JobInfoJobStateCANCELLED)
}

func parseNodeList(nodeString string) ([]string, error) {
	if nodeString == "" {
		return nil, nil
	}

	var nodes []string

	for _, part := range splitNodeString(nodeString) {
		expandedNodes, err := expandNodeRange(part)
		if err != nil {
			return nil, err
		}
		if len(expandedNodes) > 0 {
			nodes = append(nodes, expandedNodes...)
		} else {
			nodes = append(nodes, part)
		}
	}

	return nodes, nil
}

func splitNodeString(nodeString string) []string {
	var result []string
	var current strings.Builder
	bracketDepth := 0

	for _, char := range nodeString {
		switch char {
		case '[':
			bracketDepth++
			current.WriteRune(char)
		case ']':
			bracketDepth--
			current.WriteRune(char)
		case ',':
			if bracketDepth == 0 {
				// We're outside brackets, so this comma separates node patterns
				if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
					result = append(result, trimmed)
				}
				current.Reset()
			} else {
				// We're inside brackets, so this comma is part of the range spec
				current.WriteRune(char)
			}
		default:
			current.WriteRune(char)
		}
	}

	// Add the last part
	if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
		result = append(result, trimmed)
	}

	return result
}

var nodeRangeRegex = regexp.MustCompile(`^(.+)\[(.+)]$`)

func expandNodeRange(nodePattern string) ([]string, error) {
	matches := nodeRangeRegex.FindStringSubmatch(nodePattern)
	if len(matches) != 3 {
		return nil, nil
	}

	prefix := matches[1]
	rangeSpec := matches[2]

	var nodes []string

	for _, part := range strings.Split(rangeSpec, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range format in node pattern %s: %s", nodePattern, part)
			}
			start, err1 := strconv.Atoi(rangeParts[0])
			end, err2 := strconv.Atoi(rangeParts[1])
			if err1 != nil {
				return nil, fmt.Errorf("invalid start number in node pattern %s: %s: %w", nodePattern, rangeParts[0], err1)
			}
			if err2 != nil {
				return nil, fmt.Errorf("invalid end number in node pattern %s: %s: %w", nodePattern, rangeParts[1], err2)
			}
			if start > end {
				return nil, fmt.Errorf("invalid range in node pattern %s: start %d > end %d", nodePattern, start, end)
			}
			width := len(rangeParts[0])
			for i := start; i <= end; i++ {
				nodes = append(nodes, fmt.Sprintf("%s%0*d", prefix, width, i))
			}
		} else {
			num, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid number in node pattern %s: %s: %w", nodePattern, part, err)
			}
			width := len(part)
			nodes = append(nodes, fmt.Sprintf("%s%0*d", prefix, width, num))
		}
	}

	return nodes, nil
}

func convertToMetav1Time(input *api.V0041Uint64NoValStruct) *metav1.Time {
	if input == nil || input.Set == nil || !*input.Set || input.Number == nil {
		return nil
	}

	if input.Infinite != nil && *input.Infinite {
		return nil
	}

	t := time.Unix(*input.Number, 0)
	return &metav1.Time{Time: t}
}

func unixTimeToMetav1Time(input *int64) *metav1.Time {
	if input == nil || *input <= 0 {
		return nil
	}

	t := time.Unix(*input, 0)
	return &metav1.Time{Time: t}
}

// isStaleAccountingPending reports whether an accounting job record is a "zombie" — never
// started, never ended, and submitted long enough ago that it cannot represent an active job.
// Slurmdbd's no-state predicate keeps any row with time_end=0 forever (the underlying SQL is
// `(t1.time_end >= start_time OR t1.time_end = 0)`), so leftovers from a scancel that didn't
// propagate or a controller crash accumulate as permanent Prometheus series until PurgeJobAfter
// sweeps them. Dropping them at fetch time avoids that cardinality leak.
func isStaleAccountingPending(j api.V0041Job, submitCutoff int64) bool {
	if j.Time == nil {
		return false
	}
	end := derefInt64(j.Time.End)
	start := derefInt64(j.Time.Start)
	submit := derefInt64(j.Time.Submission)
	return end == 0 && start == 0 && submit > 0 && submit < submitCutoff
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// isUnallocatedNodeList reports whether the given Slurm "nodes" string represents an unallocated
// job (no nodes yet assigned). Slurmdbd returns the literal "None assigned" for pending jobs, and
// has historically used "(null)" for similar cases; the controller path may return an empty or
// whitespace-only string. Treating these as no-nodes at the conversion boundary keeps downstream
// callers (parseNodeList, slurm_node_job emission) free of per-call special cases.
func isUnallocatedNodeList(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || s == "None assigned" || s == "(null)"
}

func convertToInt(input *api.V0041Uint32NoValStruct) *int32 {
	if input == nil || input.Set == nil || !*input.Set || input.Number == nil {
		return nil
	}

	if input.Infinite != nil && *input.Infinite {
		return nil
	}

	return input.Number
}

func convertToInt64(input *api.V0041Uint64NoValStruct) *int64 {
	if input == nil || input.Set == nil || !*input.Set || input.Number == nil {
		return nil
	}

	if input.Infinite != nil && *input.Infinite {
		return nil
	}

	return input.Number
}

func tresListToString(input *api.V0041TresList) string {
	if input == nil {
		return ""
	}

	parts := make([]string, 0, len(*input))
	for _, tres := range *input {
		if tres.Count == nil {
			continue
		}

		key := tres.Type
		if tres.Name != nil && *tres.Name != "" {
			key = fmt.Sprintf("%s/%s", key, *tres.Name)
		}

		// Slurm reports memory TRES counts in MB (matching `sacct -P -o ReqTRES`); the rest are dimensionless.
		// Append "M" so parseMemoryValue interprets the value correctly - without a suffix
		// it would treat the number as bytes.
		value := strconv.FormatInt(*tres.Count, 10)
		if tres.Type == "mem" {
			value += "M"
		}

		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	return strings.Join(parts, ",")
}
