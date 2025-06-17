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
}
