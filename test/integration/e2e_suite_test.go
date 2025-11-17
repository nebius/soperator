//go:build integration

// Package integration contains integration tests for Helm charts deployment with Kind cluster and FluxCD.
//
// Environment Variables:
//   - UNSTABLE: Controls whether to use unstable (development) or stable (release) versions.
//   - "true" (default): Uses unstable versions from cr.eu-north1.nebius.cloud/soperator-unstable
//     with git commit hash suffix (e.g., 1.22.3-e0c75283)
//   - "false": Uses stable versions from cr.eu-north1.nebius.cloud/soperator
//     without git commit hash (e.g., 1.22.3)
//
// Examples:
//
//	# Run with unstable version (default)
//	go test -v -timeout 10m -tags=integration ./test/integration/
//
//	# Run with stable version
//	UNSTABLE=false go test -v -timeout 10m -tags=integration ./test/integration/
package integration

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"nebius.ai/slurm-operator/test/testenv"
)

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting Helm Integration Test Suite\n")
	RunSpecs(t, "Helm Integration Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	// Determine if we should use unstable version (default: true)
	unstable := true
	if unstableEnv := os.Getenv("UNSTABLE"); unstableEnv != "" {
		var err error
		unstable, err = strconv.ParseBool(unstableEnv)
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "Warning: Invalid UNSTABLE value '%s', defaulting to true\n", unstableEnv)
			unstable = true
		}
	}
	_, _ = fmt.Fprintf(GinkgoWriter, "Using UNSTABLE=%v\n", unstable)

	By("Installing kind CLI")
	Expect(testenv.InstallKind(ctx)).To(Succeed())

	By("Installing flux CLI")
	Expect(testenv.InstallFlux(ctx)).To(Succeed())

	By("Installing yq CLI")
	Expect(testenv.InstallYq(ctx)).To(Succeed())

	By("Ensuring clean kind cluster (delete if exists, then create)")
	_ = testenv.DeleteKindCluster(ctx)

	By("Creating kind cluster")
	Expect(testenv.CreateKindCluster(ctx)).To(Succeed())

	By(fmt.Sprintf("Syncing version files with UNSTABLE=%v", unstable))
	Expect(testenv.SyncVersion(ctx, unstable)).To(Succeed())

	By(fmt.Sprintf("Deploying FluxCD with UNSTABLE=%v", unstable))
	Expect(testenv.DeployFlux(ctx, unstable)).To(Succeed())
}, NodeTimeout(5*time.Minute))

var _ = AfterSuite(func(ctx SpecContext) {
	By("Deleting kind cluster")
	_ = testenv.DeleteKindCluster(ctx)
}, NodeTimeout(5*time.Minute))
