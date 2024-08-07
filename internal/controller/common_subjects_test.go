package controller_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

type shouldCreateAvailableSlurmCluster struct {
	ctx    context.Context
	client client.Client
	crd    *slurmv1.SlurmCluster
}

func (s shouldCreateAvailableSlurmCluster) run() {
	By("checking that the CR can be created")
	Expect(s.client.Create(s.ctx, s.crd)).Should(Succeed())

	By("checking that the Slurm Cluster can be fetched")
	createdCluster := &slurmv1.SlurmCluster{}
	eventuallyGetNamespacedObj(s.ctx, s.client, s.crd.Namespace, s.crd.Name, createdCluster)
}
