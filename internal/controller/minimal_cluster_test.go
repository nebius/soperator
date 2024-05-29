package controller_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Minimal Slurm Cluster", Ordered, func() {
	const namespace = "minimal"
	crd := minimalSlurmClusterFixture(namespace)

	ctx := context.Background()

	BeforeAll(func() {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		Expect(k8sClient.Create(context.Background(), ns)).To(Succeed())
	})

	It("should create an available Slurm Cluster", func() {
		shouldCreateAvailableSlurmCluster{
			ctx:    ctx,
			client: k8sClient,
			crd:    crd,
		}.run()
	})

	Context("Delete Slurm Cluster", func() {
		It("should successfully delete the Slurm Cluster", func() {
			Expect(k8sClient.Delete(ctx, crd)).Should(Succeed())
		})
	})

})
