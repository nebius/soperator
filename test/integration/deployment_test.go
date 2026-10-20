//go:build integration

package integration

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"nebius.ai/slurm-operator/test/testenv"
)

var _ = Describe("Local Kind Cluster with FluxCD", func() {
	JustBeforeEach(func() {
		fmt.Printf("\n▶ Running: %s\n", CurrentSpecReport().FullText())
	})

	Context("Cluster Setup", func() {
		It("should have kind cluster running", func(ctx SpecContext) {
			status, err := testenv.GetKindClusterStatus(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).To(ContainSubstring("kind-soperator-dev"))
		})

		It("should have kubectl context set correctly", func() {
			cmd := exec.Command("kubectl", "config", "current-context")
			output, err := testenv.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("kind-soperator-dev"))
		})

		It("should have all nodes ready", func() {
			cmd := exec.Command("kubectl", "get", "nodes", "-o", "jsonpath={.items[*].status.conditions[?(@.type=='Ready')].status}")
			output, err := testenv.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			statuses := strings.Fields(output)
			for _, status := range statuses {
				Expect(status).To(Equal("True"))
			}
		})
	})

	Context("CRDs Installation", func() {
		It("should have FluxCD CRDs installed", func(ctx SpecContext) {
			Eventually(ctx, func() bool {
				return testenv.IsFluxCDCRDsInstalled(ctx)
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("should have Kruise CRDs installed", func(ctx SpecContext) {
			Eventually(ctx, func() bool {
				return testenv.IsKruiseCRDsInstalled(ctx)
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("should have SlurmCluster CRDs installed", func(ctx SpecContext) {
			Eventually(ctx, func() bool {
				return testenv.IsSlurmClusterCRDsInstalled(ctx)
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("should have cert-manager CRDs installed", func(ctx SpecContext) {
			Eventually(ctx, func() bool {
				return testenv.IsCertManagerCRDsInstalled(ctx)
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
		})
	})

	Context("FluxCD HelmReleases", func() {
		requiredHelmReleases := []string{
			"soperator-fluxcd-ns",
			"soperator-fluxcd-kruise",
			"soperator-fluxcd",
			"soperator-fluxcd-slurm-cluster-storage",
			"soperator-fluxcd-slurm-cluster",
			"soperator-fluxcd-soperatorchecks",
		}

		for _, releaseName := range requiredHelmReleases {
			It(fmt.Sprintf("should have HelmRelease %s in Ready status", releaseName), func() {
				Eventually(func() bool {
					cmd := exec.Command("kubectl", "get", "helmrelease", releaseName,
						"-n", "flux-system",
						"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
					output, err := testenv.Run(cmd)
					if err != nil {
						GinkgoWriter.Printf("Error checking HelmRelease %s: %v\n", releaseName, err)
						return false
					}
					return strings.TrimSpace(output) == "True"
				}, 10*time.Minute, 10*time.Second).Should(BeTrue(),
					fmt.Sprintf("HelmRelease %s should be Ready", releaseName))
			})
		}

		It("should have all HelmReleases reconciled successfully", func(ctx SpecContext) {
			Eventually(ctx, func(g Gomega) {
				cmd := exec.CommandContext(ctx, "kubectl", "get", "helmreleases", "-n", "flux-system",
					"-o", "jsonpath={.items[*].metadata.name}")
				output, err := testenv.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				releases := strings.Fields(output)
				g.Expect(releases).To(ContainElements(requiredHelmReleases))

				for _, release := range releases {
					// ActiveChecks can run long jobs beyond the integration test timeout.
					if release == "soperator-fluxcd-soperator-activechecks" {
						continue
					}
					statusCmd := exec.CommandContext(ctx, "kubectl", "get", "helmrelease", release,
						"-n", "flux-system",
						"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
					status, err := testenv.Run(statusCmd)
					g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("get status for HelmRelease %s", release))

					if strings.TrimSpace(status) != "True" {
						msgCmd := exec.CommandContext(ctx, "kubectl", "get", "helmrelease", release,
							"-n", "flux-system",
							"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].message}")
						msg, _ := testenv.Run(msgCmd)
						g.Expect(strings.TrimSpace(status)).To(Equal("True"),
							fmt.Sprintf("HelmRelease %s is not Ready: %s", release, strings.TrimSpace(msg)))
					}
				}
			}, 5*time.Minute, 5*time.Second).Should(Succeed())
		})
	})

	Context("Operator Deployment", func() {
		It("should have soperator namespace created", func() {
			cmd := exec.Command("kubectl", "get", "namespace", "soperator-system")
			_, err := testenv.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should have cert-manager pods running", func() {
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-n", "cert-manager-system",
					"-o", "jsonpath={.items[*].status.phase}")
				output, err := testenv.Run(cmd)
				if err != nil {
					return false
				}
				phases := strings.Fields(output)
				if len(phases) == 0 {
					return false
				}
				for _, phase := range phases {
					if phase != "Running" {
						return false
					}
				}
				return true
			}, 3*time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("should have webhook certificate created", func() {
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "certificate", "soperator-controller-serving-cert",
					"-n", "soperator-system",
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := testenv.Run(cmd)
				if err != nil {
					return false
				}
				return strings.TrimSpace(output) == "True"
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
		})

		It("should have soperator controller manager pod running", func() {
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-n", "soperator-system",
					"-l", "control-plane=controller-manager",
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := testenv.Run(cmd)
				if err != nil {
					return false
				}
				return strings.TrimSpace(output) == "Running"
			}, 5*time.Minute, 5*time.Second).Should(BeTrue())
		})
	})

	Context("Slurm Cluster", func() {
		It("should have SlurmCluster resource created", func() {
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "slurmclusters", "-A")
				_, err := testenv.Run(cmd)
				return err == nil
			}, 5*time.Minute, 10*time.Second).Should(BeTrue())
		})

	})

	Context("Health Checks", func() {
		It("should have all pods in flux-system namespace running", func() {
			cmd := exec.Command("kubectl", "get", "pods", "-n", "flux-system",
				"-o", "jsonpath={.items[*].status.phase}")
			output, err := testenv.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			phases := strings.Fields(output)
			for _, phase := range phases {
				Expect(phase).To(Equal("Running"))
			}
		})

		It("should have no failed HelmReleases", func(ctx SpecContext) {
			Eventually(ctx, func(g Gomega) {
				cmd := exec.CommandContext(ctx, "kubectl", "get", "helmreleases", "-n", "flux-system",
					"-o", "jsonpath={.items[*].metadata.name}")
				output, err := testenv.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				releases := strings.Fields(output)
				g.Expect(releases).NotTo(BeEmpty())
				var failedReleases []string

				for _, release := range releases {
					// ActiveChecks can run long jobs beyond the integration test timeout.
					if release == "soperator-fluxcd-soperator-activechecks" {
						continue
					}
					statusCmd := exec.CommandContext(ctx, "kubectl", "get", "helmrelease", release,
						"-n", "flux-system",
						"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
					status, err := testenv.Run(statusCmd)
					g.Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("get status for HelmRelease %s", release))
					if strings.TrimSpace(status) != "True" {
						msgCmd := exec.CommandContext(ctx, "kubectl", "get", "helmrelease", release,
							"-n", "flux-system",
							"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].message}")
						msg, _ := testenv.Run(msgCmd)
						failedReleases = append(failedReleases, fmt.Sprintf("%s: %s", release, strings.TrimSpace(msg)))
					}
				}

				g.Expect(failedReleases).To(BeEmpty(),
					fmt.Sprintf("The following HelmReleases are not Ready:\n%s", strings.Join(failedReleases, "\n")))
			}, 5*time.Minute, 5*time.Second).Should(Succeed())
		})
	})
})
