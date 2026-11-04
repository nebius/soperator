package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoginCgroupResolver(t *testing.T) {
	t.Parallel()

	resolver := loginCgroupResolver{base: "/kubepods/pod/container"}
	tests := []struct {
		name       string
		uid        uint32
		wantParent string
	}{
		{
			name:       "user",
			uid:        1004,
			wantParent: "/kubepods/pod/container/users/user-1004/docker",
		},
		{
			name:       "root infrastructure",
			uid:        0,
			wantParent: "/kubepods/pod/container/docker-unattributed",
		},
		{
			name:       "system service",
			uid:        999,
			wantParent: "/kubepods/pod/container/docker-unattributed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			resolution, err := resolver.Resolve(peerCredentials{uid: test.uid})
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if resolution.parent != test.wantParent {
				t.Fatalf("parent = %q, want %q", resolution.parent, test.wantParent)
			}
		})
	}
}

func TestWorkerCgroupResolver(t *testing.T) {
	t.Parallel()

	procRoot := t.TempDir()
	writePeerCgroup := func(t *testing.T, pid int32, contents string) {
		t.Helper()
		directory := filepath.Join(procRoot, strconv.FormatInt(int64(pid), 10))
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create peer proc directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(directory, "cgroup"), []byte(contents), 0o644); err != nil {
			t.Fatalf("write peer cgroup file: %v", err)
		}
	}

	resolver := workerCgroupResolver{procRoot: procRoot}
	t.Run("Slurm 25.11 task", func(t *testing.T) {
		writePeerCgroup(t, 42, "0::/kubepods/pod/container/slurm/uid_1004/job_77/step_0/user/task_0\n")
		resolution, err := resolver.Resolve(peerCredentials{pid: 42, uid: 1004})
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if want := "/kubepods/pod/container/slurm/uid_1004/job_77/step_0/user"; resolution.parent != want {
			t.Fatalf("parent = %q, want %q", resolution.parent, want)
		}
	})

	t.Run("Slurm 26.05 task", func(t *testing.T) {
		writePeerCgroup(t, 43, "0::/kubepods.slice/pod.scope/container.scope/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_0/user/task_0\n")
		resolution, err := resolver.Resolve(peerCredentials{pid: 43, uid: 1004})
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if want := "/kubepods.slice/pod.scope/container.scope/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_0/user"; resolution.parent != want {
			t.Fatalf("parent = %q, want %q", resolution.parent, want)
		}
	})

	t.Run("direct worker SSH", func(t *testing.T) {
		writePeerCgroup(t, 42, "0::/kubepods/pod/container\n")
		resolution, err := resolver.Resolve(peerCredentials{pid: 42, uid: 1004})
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if resolution.parent != "" {
			t.Fatalf("parent = %q, want empty", resolution.parent)
		}
		if resolution.source != "/kubepods/pod/container" {
			t.Fatalf("source = %q, want peer cgroup path", resolution.source)
		}
	})
}

func TestWorkerCgroupResolverRequiresVisiblePID(t *testing.T) {
	t.Parallel()

	resolver := workerCgroupResolver{procRoot: t.TempDir()}
	if _, err := resolver.Resolve(peerCredentials{}); err == nil || !strings.Contains(err.Error(), "PID is unavailable") {
		t.Fatalf("Resolve() error = %v, want unavailable PID", err)
	}
}

func TestUnifiedCgroupPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		contents  string
		want      string
		wantError string
	}{
		{
			name:     "unified hierarchy",
			contents: "0::/kubepods/pod/container/job_7/step_0/user/task_0\n",
			want:     "/kubepods/pod/container/job_7/step_0/user/task_0",
		},
		{
			name:      "missing unified hierarchy",
			contents:  "2:cpu:/legacy\n",
			wantError: "no unified hierarchy entry",
		},
		{
			name:      "multiple entries",
			contents:  "0::/first\n0::/second\n",
			wantError: "multiple unified hierarchy entries",
		},
		{
			name:      "unclean path",
			contents:  "0::/base/../escape\n",
			wantError: "must be clean",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := unifiedCgroupPath(test.contents)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("unifiedCgroupPath() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("path = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSlurmDockerCgroupParent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
		ok   bool
	}{
		{
			name: "Slurm 25.11 observed batch task",
			path: "/kubepods/burstable/pod123/container456/slurm/uid_1000/job_36/step_0/user/task_0",
			want: "/kubepods/burstable/pod123/container456/slurm/uid_1000/job_36/step_0/user",
			ok:   true,
		},
		{
			name: "Slurm 26.05 observed SLUID task",
			path: "/kubepods.slice/kubepods-burstable.slice/pod.scope/container.scope/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_0/user/task_0",
			want: "/kubepods.slice/kubepods-burstable.slice/pod.scope/container.scope/system.slice/slurmstepd.scope/s8G5M22WGXB100/step_0/user",
			ok:   true,
		},
		{
			name: "current Slurm batch step contract",
			path: "/kubepods/pod/container/slurm/uid_1004/job_77/step_batch/user/task_0",
			want: "/kubepods/pod/container/slurm/uid_1004/job_77/step_batch/user",
			ok:   true,
		},
		{
			name: "current Slurm interactive task contract",
			path: "/kubepods/pod/container/slurm/uid_1004/job_77/step_3/user/task_1",
			want: "/kubepods/pod/container/slurm/uid_1004/job_77/step_3/user",
			ok:   true,
		},
		{
			name: "external step",
			path: "/kubepods/pod/container/slurm/uid_1004/job_77/step_extern/user/task_0",
			want: "/kubepods/pod/container/slurm/uid_1004/job_77/step_extern/user",
			ok:   true,
		},
		{
			name: "direct worker SSH",
			path: "/kubepods/pod/container",
		},
		{
			name: "unrelated user component",
			path: "/kubepods/pod/container/job_77/user/task_0",
		},
		{
			name: "unrelated job and step names after user",
			path: "/kubepods/pod/container/user/job_77/step_0",
		},
		{
			name: "step before job",
			path: "/kubepods/pod/container/step_0/job_77/user/task_0",
		},
		{
			name: "nonnumeric job",
			path: "/kubepods/pod/container/job_other/step_0/user/task_0",
		},
		{
			name: "unknown step",
			path: "/kubepods/pod/container/job_77/step_other/user/task_0",
		},
		{
			name: "SLUID outside slurmstepd scope",
			path: "/kubepods/pod/container/other.scope/s8G5M22WGXB100/step_0/user/task_0",
		},
		{
			name: "malformed SLUID",
			path: "/kubepods/pod/container/system.slice/slurmstepd.scope/s8G5M22WGXB10O/step_0/user/task_0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := slurmDockerCgroupParent(test.path)
			if ok != test.ok || got != test.want {
				t.Fatalf("slurmDockerCgroupParent(%q) = (%q, %t), want (%q, %t)", test.path, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestNewCgroupResolverRejectsUnknownMode(t *testing.T) {
	t.Parallel()

	_, err := newCgroupResolver(context.Background(), config{mode: "unknown"})
	if err == nil || !strings.Contains(err.Error(), "mode must be") {
		t.Fatalf("newCgroupResolver() error = %v, want invalid mode", err)
	}
}

func TestWaitForCgroupBase(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	filePath := filepath.Join(directory, "cgroup-base")
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(filePath, []byte("/kubepods/pod/container\n"), 0o644)
	}()
	got, err := waitForCgroupBase(context.Background(), filePath, 2*time.Second)
	if err != nil {
		t.Fatalf("waitForCgroupBase() error = %v", err)
	}
	if want := "/kubepods/pod/container"; got != want {
		t.Fatalf("cgroup base = %q, want %q", got, want)
	}
}
