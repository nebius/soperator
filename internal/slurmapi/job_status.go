package slurmapi

import (
	"time"

	slurmapispec "github.com/SlinkyProject/slurm-client/api/v0041"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type JobStatus struct {
	Id               *int32       `json:"id,omitempty"`
	Name             *string      `json:"name,omitempty"`
	State            string       `json:"jobState,omitempty"`
	StateReason      *string      `json:"stateReason,omitempty"`
	IsTerminateState bool         `json:"isTerminateState,omitempty"`
	SubmitTime       *metav1.Time `json:"submitTime,omitempty"`
	StartTime        *metav1.Time `json:"startTime,omitempty"`
	EndTime          *metav1.Time `json:"endTime,omitempty"`
}

func isTerminalState(state slurmapispec.V0041JobInfoJobState) bool {
	switch state {
	case slurmapispec.V0041JobInfoJobStateCOMPLETED,
		slurmapispec.V0041JobInfoJobStateFAILED,
		slurmapispec.V0041JobInfoJobStateCANCELLED,
		slurmapispec.V0041JobInfoJobStateTIMEOUT,
		slurmapispec.V0041JobInfoJobStateOUTOFMEMORY,
		slurmapispec.V0041JobInfoJobStateBOOTFAIL,
		slurmapispec.V0041JobInfoJobStateDEADLINE,
		slurmapispec.V0041JobInfoJobStateLAUNCHFAILED,
		slurmapispec.V0041JobInfoJobStateNODEFAIL,
		slurmapispec.V0041JobInfoJobStatePREEMPTED,
		slurmapispec.V0041JobInfoJobStateRECONFIGFAIL,
		slurmapispec.V0041JobInfoJobStateREVOKED,
		slurmapispec.V0041JobInfoJobStateSPECIALEXIT,
		slurmapispec.V0041JobInfoJobStateSTOPPED:
		return true
	default:
		return false
	}
}

func convertToMetav1Time(input *slurmapispec.V0041Uint64NoValStruct) *metav1.Time {
	if input == nil || input.Set == nil || !*input.Set || input.Number == nil {
		return nil
	}

	if input.Infinite != nil && *input.Infinite {
		return nil
	}

	t := time.Unix(*input.Number, 0)
	return &metav1.Time{Time: t}
}
