package slurmapi

import (
	"time"

	slurmapispec "github.com/SlinkyProject/slurm-client/api/v0041"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SlurmJob struct {
	Id               *int32       `json:"id,omitempty"`
	ArrayId          *int32       `json:"arrayId,omitempty"`
	Name             *string      `json:"name,omitempty"`
	State            string       `json:"jobState,omitempty"`
	StateReason      *string      `json:"stateReason,omitempty"`
	IsTerminateState bool         `json:"isTerminateState,omitempty"`
	IsFailedState    bool         `json:"isFailedState,omitempty"`
	SubmitTime       *metav1.Time `json:"submitTime,omitempty"`
	StartTime        *metav1.Time `json:"startTime,omitempty"`
	EndTime          *metav1.Time `json:"endTime,omitempty"`
	NodeCount        *int32       `json:"nodeCount,omitempty"`
	NodeName         *string      `json:"nodeName,omitempty"`
}

func isTerminalState(state slurmapispec.V0041JobInfoJobState) bool {
	return isFailedState(state) || state == slurmapispec.V0041JobInfoJobStateCOMPLETED || state == slurmapispec.V0041JobInfoJobStateSTOPPED
}

func isFailedState(state slurmapispec.V0041JobInfoJobState) bool {
	switch state {
	case slurmapispec.V0041JobInfoJobStateFAILED,
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
		slurmapispec.V0041JobInfoJobStateSPECIALEXIT:
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

func convertToInt(input *slurmapispec.V0041Uint32NoValStruct) *int32 {
	if input == nil || input.Set == nil || !*input.Set || input.Number == nil {
		return nil
	}

	if input.Infinite != nil && *input.Infinite {
		return nil
	}

	return input.Number
}
