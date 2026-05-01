package slurmapi

import (
	"strings"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"k8s.io/utils/ptr"
)

type JobSource string

const (
	JobSourceController JobSource = "controller"
	JobSourceAccounting JobSource = "accounting"
)

type AccountingJobMode string

const (
	AccountingJobModePending   AccountingJobMode = "pending"
	AccountingJobModeRunning   AccountingJobMode = "running"
	AccountingJobModeCompleted AccountingJobMode = "completed"
	AccountingJobModeAll       AccountingJobMode = "all"
)

type ListJobsParams struct {
	Sources           []JobSource
	AccountingJobMode AccountingJobMode
}

var completedAccountingJobStates = strings.Join([]string{
	string(api.V0041JobStateCurrentBOOTFAIL),
	string(api.V0041JobStateCurrentCANCELLED),
	string(api.V0041JobStateCurrentCOMPLETED),
	string(api.V0041JobStateCurrentDEADLINE),
	string(api.V0041JobStateCurrentFAILED),
	string(api.V0041JobStateCurrentLAUNCHFAILED),
	string(api.V0041JobStateCurrentNODEFAIL),
	string(api.V0041JobStateCurrentOUTOFMEMORY),
	string(api.V0041JobStateCurrentPREEMPTED),
	string(api.V0041JobStateCurrentRECONFIGFAIL),
	string(api.V0041JobStateCurrentREVOKED),
	string(api.V0041JobStateCurrentSPECIALEXIT),
	string(api.V0041JobStateCurrentSTOPPED),
	string(api.V0041JobStateCurrentTIMEOUT),
}, ",")

func (p ListJobsParams) normalized() ListJobsParams {
	if len(p.Sources) == 0 {
		p.Sources = []JobSource{JobSourceController}
	}
	seen := make(map[JobSource]struct{}, len(p.Sources))
	sources := make([]JobSource, 0, len(p.Sources))
	for _, source := range p.Sources {
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		sources = append(sources, source)
	}
	p.Sources = sources

	if p.AccountingJobMode == "" {
		p.AccountingJobMode = AccountingJobModeAll
	}
	return p
}

func (p ListJobsParams) accountingStateFilter() *string {
	switch p.AccountingJobMode {
	case AccountingJobModePending:
		return ptr.To(string(api.V0041JobStateCurrentPENDING))
	case AccountingJobModeRunning:
		return ptr.To(string(api.V0041JobStateCurrentRUNNING))
	case AccountingJobModeCompleted:
		return ptr.To(completedAccountingJobStates)
	default:
		return nil
	}
}
