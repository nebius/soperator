package sharedsteps

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
	corev1 "k8s.io/api/core/v1"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/kubeobjects"
)

type filesystemLayoutMount struct {
	Mount, Name, Path, Type, Mode string
	Source                        *corev1.VolumeSource
	Observed                      filesystemLayoutProbe
}

type filesystemLayoutExpectation struct {
	Mount, Path, Type, Mode string
}

type filesystemLayoutNode struct {
	Pod       corev1.Pod
	Container string
	Mounts    []filesystemLayoutMount
}

type filesystemLayoutProbe struct {
	Target string      `json:"target"`
	Source string      `json:"source"`
	Type   string      `json:"fstype"`
	Size   json.Number `json:"size"`
	Device string      `json:"maj:min"`
	Mode   string      `json:"-"`
}

// FilesystemLayout checks the pre-provisioned scenario-210 storage fixture.
type FilesystemLayout struct {
	loginWorkload
	selector *framework.WorkerSelector
	worker   framework.WorkerInfo
	nodes    []filesystemLayoutNode
	markers  []struct {
		Node filesystemLayoutNode
		Dir  string
	}
}

func NewFilesystemLayout(info *framework.ClusterInfo, runtime framework.Runtime, kubectl *framework.KubectlClient, selector *framework.WorkerSelector) *FilesystemLayout {
	return &FilesystemLayout{loginWorkload: loginWorkload{info: info, runtime: runtime, kubectl: kubectl}, selector: selector}
}

func (s *FilesystemLayout) RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a GPU worker is available for filesystem checks$`, s.selectGPUWorker)
	sc.Step(`^the filesystem layout is inspected on one login node and one GPU worker$`, s.selectNodes)
	sc.Step(`^the following mounts have their expected filesystem types and permissions:$`, s.checkMounts)
	sc.Step(`^each mount has the capacity expected from its configured backing storage$`, s.checkCapacities)
	sc.Step(`^small files can be created, read and deleted on every writable data mount$`, s.checkIO)
	sc.Step(`^the login node and worker exchange files in both directions through NFS home and shared data$`, s.checkSharedIO)
}

func (s *FilesystemLayout) CleanupAndReset(ctx context.Context) {
	for _, marker := range s.markers {
		if _, err := s.run(ctx, marker.Node, "rm -rf -- "+framework.ShellQuote(marker.Dir)); err != nil {
			s.runtime.Logf("Cleanup: remove filesystem marker %s on %s: %v", marker.Dir, marker.Node.Pod.Name, err)
			framework.RecordWorkerCleanupError(ctx, err)
		}
	}
	s.worker = framework.WorkerInfo{}
	s.nodes, s.markers = nil, nil
}

func (s *FilesystemLayout) selectGPUWorker(ctx context.Context) error {
	workers, err := s.selector.PickGPUWorkers(ctx, 1)
	if err != nil {
		return framework.SkipIfInsufficientWorkers(s.runtime, err)
	}
	s.worker = workers[0]
	return nil
}

func filesystemLayoutMounts(config []kubeobjects.NodeVolumeMount, worker bool) ([]filesystemLayoutMount, error) {
	var mounts = []filesystemLayoutMount{
		{Mount: "Jail root", Name: "jail"},
		{Mount: "Temporary storage", Name: "tmp-disk"},
		{Mount: "Memory", Name: "in-memory"},
	}
	for _, mount := range config {
		var role string
		switch {
		case mount.Name == "home-dir" || mount.Name == "home":
			role = "NFS home"
		case mount.Name == "data":
			role = "Shared data"
		case mount.Name == containerRuntimeSubmountName || mount.Name == "login-image-storage":
			role = "Image storage"
		case worker && (mount.VolumeClaimTemplateSpec != nil || mount.Name == framework.LocalDataVolumeName || mount.Name == framework.LocalNVMeVolumeName):
			role = "Scratch"
		default:
			continue
		}
		mountPath, err := cleanAbsolutePath(mount.MountPath)
		if err != nil {
			return nil, err
		}
		if mount.ReadOnly {
			return nil, fmt.Errorf("filesystem fixture mount %s at %s is configured read-only", mount.Name, mountPath)
		}
		mounts = append(mounts, filesystemLayoutMount{Mount: role, Name: mount.Name, Path: mountPath, Source: mount.VolumeSource})
	}
	if worker {
		mounts = append(mounts, filesystemLayoutMount{Mount: "NVIDIA driver", Name: "nvidia"})
	}
	return mounts, nil
}

func (s *FilesystemLayout) selectNodes(ctx context.Context) error {
	var cluster kubeobjects.SlurmCluster
	if err := s.kubectl.GetJSON(ctx, &cluster, "get", "slurmcluster", s.info.SlurmClusterName, "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return err
	}
	loginConfig := cluster.Spec.SlurmNodes.Login.Volumes.JailSubMounts
	for i := range loginConfig {
		for _, volume := range cluster.Spec.VolumeSources {
			if volume.Name == loginConfig[i].VolumeSourceName {
				source := volume.VolumeSource
				loginConfig[i].VolumeSource = &source
			}
		}
	}
	loginMounts, err := filesystemLayoutMounts(loginConfig, false)
	if err != nil {
		return err
	}
	var nodeSet kubeobjects.NodeSet
	if err := s.kubectl.GetJSON(ctx, &nodeSet, "get", "nodeset", s.worker.NodeSetName, "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return err
	}
	workerMounts, err := filesystemLayoutMounts(nodeSet.Spec.Slurmd.Volumes.JailSubMounts, true)
	if err != nil {
		return err
	}
	pod, err := s.kubectl.WorkerPodForSlurmNode(ctx, s.worker.Name)
	if err != nil {
		return err
	}
	var worker corev1.Pod
	if err := s.kubectl.GetJSON(ctx, &worker, "get", "pod", pod.PodName, "-n", framework.SoperatorNamespace, "-o", "json"); err != nil {
		return err
	}
	logins, err := s.loginPods(ctx)
	if err != nil {
		return err
	}
	for _, login := range logins {
		if login.DeletionTimestamp == nil && slices.ContainsFunc(login.Status.Conditions, func(c corev1.PodCondition) bool {
			return c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue
		}) {
			s.nodes = []filesystemLayoutNode{
				{Pod: login, Container: loginSSHDContainer, Mounts: loginMounts},
				{Pod: worker, Container: slurmdContainerName, Mounts: workerMounts},
			}
			return nil
		}
	}
	return fmt.Errorf("select filesystem-layout login node: no configured login pod is Ready")
}

func (s *FilesystemLayout) run(ctx context.Context, node filesystemLayoutNode, command string) (string, error) {
	return s.runtime.Kubectl().Run(ctx, "exec", "-n", node.Pod.Namespace, node.Pod.Name, "-c", node.Container, "--", "bash", "-c", command)
}

func (s *FilesystemLayout) probe(ctx context.Context, node filesystemLayoutNode, target string) (filesystemLayoutProbe, error) {
	command := framework.ShellJoin("findmnt", "--json", "--bytes", "--mountpoint", target, "--output", "TARGET,SOURCE,FSTYPE,SIZE,MAJ:MIN") +
		" && stat -c '%a' -- " + framework.ShellQuote(target)
	out, err := s.run(ctx, node, command)
	if err != nil {
		return filesystemLayoutProbe{}, fmt.Errorf("probe mount %s on %s: %w", target, node.Pod.Name, err)
	}
	return parseFilesystemLayoutProbe(out, target)
}

func parseFilesystemLayoutProbe(out, target string) (filesystemLayoutProbe, error) {
	var result struct {
		Filesystems []filesystemLayoutProbe `json:"filesystems"`
	}
	decoder := json.NewDecoder(strings.NewReader(out))
	if err := decoder.Decode(&result); err != nil {
		return filesystemLayoutProbe{}, fmt.Errorf("decode filesystem probe for %s: %w", target, err)
	}
	if len(result.Filesystems) != 1 || result.Filesystems[0].Target != target {
		return filesystemLayoutProbe{}, fmt.Errorf("expected exactly one mount at %s, got %s", target, out)
	}
	probe := result.Filesystems[0]
	probe.Mode = strings.TrimSpace(out[decoder.InputOffset():])
	if size, err := probe.Size.Int64(); err != nil || size <= 0 || probe.Source == "" || probe.Device == "" || probe.Type == "" {
		return filesystemLayoutProbe{}, fmt.Errorf("invalid filesystem probe for %s: %s", target, out)
	}
	return probe, nil
}

func parseFilesystemLayoutTable(table *godog.Table) ([]filesystemLayoutExpectation, error) {
	if table == nil || len(table.Rows) < 2 {
		return nil, fmt.Errorf("provide filesystem expectations below a table header")
	}
	header := table.Rows[0].Cells
	if len(header) != 4 || header[0].Value != "Mount" || header[1].Value != "Path" || header[2].Value != "Filesystem" || header[3].Value != "Mode" {
		return nil, fmt.Errorf("use the header Mount | Path | Filesystem | Mode")
	}
	var expectations []filesystemLayoutExpectation
	for i, row := range table.Rows[1:] {
		if len(row.Cells) != 4 {
			return nil, fmt.Errorf("parse filesystem row %d: expected 4 cells", i+2)
		}
		var values [4]string
		for j, cell := range row.Cells {
			values[j] = strings.TrimSpace(cell.Value)
			if values[j] == "" {
				return nil, fmt.Errorf("parse filesystem row %d: empty %s", i+2, header[j].Value)
			}
		}
		expectations = append(expectations, filesystemLayoutExpectation{Mount: values[0], Path: values[1], Type: values[2], Mode: values[3]})
	}
	return expectations, nil
}

func (s *FilesystemLayout) checkMounts(ctx context.Context, table *godog.Table) error {
	expectations, err := parseFilesystemLayoutTable(table)
	if err != nil {
		return err
	}
	for i, node := range s.nodes {
		var checked []filesystemLayoutMount
		for _, expected := range expectations {
			workerOnly := expected.Mount == "Scratch" || expected.Mount == "NVIDIA driver"
			if node.Container == loginSSHDContainer && workerOnly {
				continue
			}
			found := false
			for _, mount := range node.Mounts {
				if mount.Mount != expected.Mount {
					continue
				}
				found = true
				if expected.Path != "configured" {
					mount.Path, err = cleanAbsolutePath(expected.Path)
					if err != nil {
						return err
					}
				} else if mount.Path == "" {
					return fmt.Errorf("resolve configured path for %s on %s", expected.Mount, node.Pod.Name)
				}
				mount.Type, mount.Mode = expected.Type, expected.Mode
				if mount.Type == "configured" {
					mount.Type = ""
				}
				observed, err := s.probe(ctx, node, path.Join("/mnt/jail", mount.Path))
				if err != nil {
					return err
				}
				if mount.Type != "" && observed.Type != mount.Type || observed.Mode != mount.Mode {
					return fmt.Errorf("pod %s mount %s: expected type=%s mode=%s, observed type=%s mode=%s", node.Pod.Name, mount.Path, mount.Type, mount.Mode, observed.Type, observed.Mode)
				}
				mount.Observed = observed
				checked = append(checked, mount)
				s.runtime.Logf("Filesystem layout: %s %s source=%s type=%s size=%s mode=%s", node.Pod.Name, mount.Path, observed.Source, observed.Type, observed.Size, observed.Mode)
			}
			if !found {
				if expected.Mount == "Image storage" {
					s.runtime.Logf("Filesystem layout: %s image storage is not configured; skipping optional check", node.Pod.Name)
					continue
				}
				return fmt.Errorf("%s is not configured on pod %s", expected.Mount, node.Pod.Name)
			}
		}
		s.nodes[i].Mounts = checked
	}
	return nil
}

func (s *FilesystemLayout) checkCapacities(ctx context.Context) error {
	for _, node := range s.nodes {
		for _, mount := range node.Mounts {
			if err := s.checkMountCapacity(ctx, node, mount, mount.Observed); err != nil {
				return fmt.Errorf("pod %s mount %s: %w", node.Pod.Name, mount.Path, err)
			}
		}
	}
	return nil
}

func (s *FilesystemLayout) checkMountCapacity(ctx context.Context, node filesystemLayoutNode, mount filesystemLayoutMount, observed filesystemLayoutProbe) error {
	if mount.Name == "tmp-disk" || mount.Name == "nvidia" {
		backing, err := s.probe(ctx, node, mount.Path)
		if err != nil {
			return err
		}
		if observed.Device != backing.Device || observed.Source != backing.Source || observed.Type != backing.Type || observed.Size != backing.Size {
			return fmt.Errorf("jail mount %+v differs from its backing mount %+v", observed, backing)
		}
		return nil
	}
	idx := slices.IndexFunc(node.Pod.Spec.Volumes, func(v corev1.Volume) bool { return v.Name == mount.Name })
	if idx < 0 {
		return fmt.Errorf("configured volume %s is absent from the pod", mount.Name)
	}
	volume := node.Pod.Spec.Volumes[idx]
	if expected := mount.Source; expected != nil && expected.PersistentVolumeClaim != nil {
		if volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ClaimName != expected.PersistentVolumeClaim.ClaimName {
			return fmt.Errorf("volume %s does not use configured claim %s", mount.Name, expected.PersistentVolumeClaim.ClaimName)
		}
	}
	if volume.EmptyDir != nil {
		if volume.EmptyDir.SizeLimit == nil || volume.EmptyDir.SizeLimit.Sign() <= 0 {
			return fmt.Errorf("volume %s has no configured size limit", mount.Name)
		}
		return validateFilesystemLayoutCapacity(observed, volume.EmptyDir.SizeLimit.Value(), mount.Name == framework.LocalNVMeVolumeName)
	}
	if volume.PersistentVolumeClaim == nil {
		return fmt.Errorf("volume %s has no PVC or sized emptyDir", mount.Name)
	}
	namespace, claimName := node.Pod.Namespace, volume.PersistentVolumeClaim.ClaimName
	if mount.Type == "nfs4" {
		// The NFS client claim declares a placeholder size; the export is backed
		// by this server PVC in both the Pro and Managed fixtures.
		namespace, claimName = "nfs-system", "nfs-server-storage"
	}
	var claim corev1.PersistentVolumeClaim
	if err := s.kubectl.GetJSON(ctx, &claim, "get", "pvc", claimName, "-n", namespace, "-o", "json"); err != nil {
		return err
	}
	if claim.Status.Phase != corev1.ClaimBound {
		return fmt.Errorf("claim %s/%s is %s, expected Bound", namespace, claimName, claim.Status.Phase)
	}
	size := claim.Spec.Resources.Requests[corev1.ResourceStorage]
	return validateFilesystemLayoutCapacity(observed, size.Value(), false)
}

func validateFilesystemLayoutCapacity(observed filesystemLayoutProbe, expected int64, atLeast bool) error {
	size, err := strconv.ParseInt(string(observed.Size), 10, 64)
	if err != nil || expected <= 0 || size < expected-expected/20 || (!atLeast && size > expected) {
		return fmt.Errorf("filesystem capacity is %s bytes, expected %d bytes allowing 5%% filesystem overhead (at least=%t)", observed.Size, expected, atLeast)
	}
	return nil
}

func (s *FilesystemLayout) checkIO(ctx context.Context) error {
	for _, node := range s.nodes {
		for _, mount := range node.Mounts {
			if mount.Path == "/" || mount.Name == "nvidia" {
				continue
			}
			dir := path.Join("/mnt/jail", mount.Path, "soperator-e2e-filesystem-"+rand.Text())
			if err := s.writeMarker(ctx, node, dir); err != nil {
				return err
			}
			command := "test \"$(cat -- " + framework.ShellQuote(path.Join(dir, "marker")) + ")\" = filesystem-layout && rm -- " +
				framework.ShellQuote(path.Join(dir, "marker")) + " && rmdir -- " + framework.ShellQuote(dir)
			if _, err := s.run(ctx, node, command); err != nil {
				return fmt.Errorf("read/delete marker at %s on %s: %w", mount.Path, node.Pod.Name, err)
			}
		}
	}
	return nil
}

func (s *FilesystemLayout) writeMarker(ctx context.Context, node filesystemLayoutNode, dir string) error {
	s.markers = append(s.markers, struct {
		Node filesystemLayoutNode
		Dir  string
	}{node, dir})
	_, err := s.run(ctx, node, "mkdir -- "+framework.ShellQuote(dir)+" && printf '%s\\n' filesystem-layout > "+framework.ShellQuote(path.Join(dir, "marker")))
	return err
}

func (s *FilesystemLayout) checkSharedIO(ctx context.Context) error {
	for _, fsType := range []string{"nfs4", "virtiofs"} {
		token := "soperator-e2e-filesystem-" + rand.Text()
		for i, writer := range s.nodes {
			reader := s.nodes[1-i]
			sharedMount := func(m filesystemLayoutMount) bool { return m.Type == fsType && m.Path != "/" }
			writerMount := writer.Mounts[slices.IndexFunc(writer.Mounts, sharedMount)]
			readerMount := reader.Mounts[slices.IndexFunc(reader.Mounts, sharedMount)]
			dir := path.Join("/mnt/jail", writerMount.Path, token)
			if err := s.writeMarker(ctx, writer, dir); err != nil {
				return err
			}
			file := path.Join("/mnt/jail", readerMount.Path, token, "marker")
			if _, err := s.run(ctx, reader, "test \"$(cat -- "+framework.ShellQuote(file)+")\" = filesystem-layout"); err != nil {
				return fmt.Errorf("read %s marker from %s on %s: %w", writerMount.Path, writer.Pod.Name, reader.Pod.Name, err)
			}
			if _, err := s.run(ctx, writer, "rm -rf -- "+framework.ShellQuote(dir)); err != nil {
				return err
			}
		}
	}
	return nil
}
