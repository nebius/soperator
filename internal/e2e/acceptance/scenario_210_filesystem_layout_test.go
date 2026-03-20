//go:build acceptance

package acceptance

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func filesystemLayoutTest(ctx SpecContext) {
	var workerName string

	suite.Given(ctx, "a worker is selected for filesystem inspection", func(_ SpecContext) {
		worker, err := suite.AnyWorker()
		Expect(err).NotTo(HaveOccurred())

		workerName = worker.Name
		suite.Logf("inspecting filesystem layout on worker %s", workerName)
	})

	suite.Then(ctx, "common mountpoints exist inside the jail", func(ctx SpecContext) {
		for _, path := range []string{"/", "/tmp", "/mnt/memory"} {
			_, err := suite.ExecJail(ctx, fmt.Sprintf("mountpoint %s && stat -c '%%a %%n' %s", path, path))
			Expect(err).NotTo(HaveOccurred(), "expected jail path %s to be mounted and stat-able", path)
		}
	})

	suite.And(ctx, "common mountpoints exist on the selected worker", func(ctx SpecContext) {
		for _, path := range []string{"/", "/tmp", "/mnt/memory", "/run/nvidia/driver"} {
			_, err := suite.ExecWorker(ctx, workerName, fmt.Sprintf("mountpoint %s && stat -c '%%a %%n' %s", path, path))
			Expect(err).NotTo(HaveOccurred(), "expected worker path %s to be mounted and stat-able", path)
		}
	})

	suite.And(ctx, "optional image storage is validated when present", func(ctx SpecContext) {
		out, err := suite.ExecWorker(ctx, workerName, "if mountpoint /mnt/image-storage >/dev/null 2>&1; then stat -c '%a %n' /mnt/image-storage; fi")
		Expect(err).NotTo(HaveOccurred())
		if out != "" {
			Expect(out).To(ContainSubstring("/mnt/image-storage"))
		}
	})
}
