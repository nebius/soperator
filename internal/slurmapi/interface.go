package slurmapi

import (
	"context"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
)

type Client interface {
	api.ClientWithResponsesInterface
	ListNodes(ctx context.Context) ([]Node, error)
	GetNode(ctx context.Context, nodeName string) (Node, error)
	GetJobsByID(ctx context.Context, jobID string) ([]Job, error)
	ListJobs(ctx context.Context) ([]Job, error)
	GetDiag(ctx context.Context) (*api.V0041OpenapiDiagResp, error)
	PostMaintenanceReservation(ctx context.Context, name string, nodeList []string) error
	GetReservation(ctx context.Context, name string) (Reservation, error)
	StopReservation(ctx context.Context, name string) error
}
