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
	SubmitTime     *metav1.Time
	StartTime      *metav1.Time
	EndTime        *metav1.Time
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
	if apiJob.MailUser != nil && job.UserName != *apiJob.MailUser {
		job.UserMail = *apiJob.MailUser
	}

	if apiJob.StandardError != nil {
		job.StandardError = *apiJob.StandardError
	}

	if apiJob.StandardOutput != nil {
		job.StandardOutput = *apiJob.StandardOutput
	}

	if apiJob.Nodes != nil {
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
	job.SubmitTime = convertToMetav1Time(apiJob.SubmitTime)
	job.StartTime = convertToMetav1Time(apiJob.StartTime)
	job.EndTime = convertToMetav1Time(apiJob.EndTime)

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

func (j Job) GetArrayTaskIDString() string {
	if j.ArrayTaskID == nil {
		return ""
	}
	return strconv.Itoa(int(*j.ArrayTaskID))
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

func convertToInt(input *api.V0041Uint32NoValStruct) *int32 {
	if input == nil || input.Set == nil || !*input.Set || input.Number == nil {
		return nil
	}

	if input.Infinite != nil && *input.Infinite {
		return nil
	}

	return input.Number
}
