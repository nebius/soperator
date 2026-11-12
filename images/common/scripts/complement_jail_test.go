package scripts_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Black-box tests for complement_jail.sh. Every run executes the unmodified script inside a
// private mount namespace with a throwaway chroot ("sandbox"): the host's /usr is bind-mounted
// read-only, /etc, /opt, /run and /tmp are empty scratch directories, and the jail is a plain
// directory. System tools the script drives (mount, chroot, findmnt, nvidia-container-cli,
// ldconfig, dpkg-divert, ...) are replaced by mocks on PATH that log their arguments. The mocked
// nvidia-container-cli and the libdummy bind use the real mount(8), so the script's mountinfo
// checks see real bind mounts, which disappear when the namespace exits.
//
// The sandbox needs either root or unprivileged user namespaces (unshare -Urm). Without them the
// tests skip, unless COMPLEMENT_JAIL_TEST_REQUIRE is set. Locally on macOS run them in Docker, e.g.
//
//	GOOS=linux go test -c -o /tmp/cj.test ./images/common/scripts/ && \
//	docker run --rm --user nobody --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
//	  -v "$PWD:$PWD:ro" -v /tmp/cj.test:/cj.test:ro <ubuntu image with jq and python3> /cj.test -test.run TestComplementJail
//
// The repository is mounted at its host path because the tests locate the script next to this file.
// The container needs jq, util-linux and python3 (the script's dpkg lock helper).

const (
	cjArch          = "x86_64"
	cjLibDir        = "usr/lib/" + cjArch + "-linux-gnu"
	cjDriverVersion = "580.65.06"
	cjMarker        = "soperator gpu placeholder\n"
	cjDivertSubdir  = "soperator-diverted"
)

// Fake driver files. Libraries and nvidia-smi live under /usr/local (a writable scratch bind in the
// sandbox) so the script's chmod --reference can read their modes; firmware is listed under /lib,
// which only exists in the mock host tree, like a path the CLI lists but the worker cannot stat.
var cjDriverFiles = []struct {
	hostPath string
	mode     os.FileMode
}{
	{"/usr/local/lib/" + cjArch + "-linux-gnu/libcuda.so." + cjDriverVersion, 0o644},
	{"/usr/local/lib/" + cjArch + "-linux-gnu/libnvidia-ml.so." + cjDriverVersion, 0o644},
	{"/usr/local/lib/" + cjArch + "-linux-gnu/vdpau/libvdpau_nvidia.so." + cjDriverVersion, 0o644},
	{"/usr/local/bin/nvidia-smi", 0o755},
	{"/lib/firmware/nvidia/" + cjDriverVersion + "/gsp_ga10x.bin", 0o644},
}

var cjFlatLibs = []string{
	"libcuda.so." + cjDriverVersion,
	"libnvidia-ml.so." + cjDriverVersion,
	"libvdpau_nvidia.so." + cjDriverVersion,
}

type cjSandbox struct {
	t          *testing.T
	dir        string
	root       string
	jail       string
	upper      string
	mock       string
	mockHost   string
	usrLocal   string
	unshare    []string
	extraBinds []string
	runs       atomic.Int64
}

type cjRunOpts struct {
	worker bool
	env    map[string]string
	// onLine is called for every output line while the script runs
	onLine func(line string)
}

type cjRunResult struct {
	output   string
	exitCode int
	logDir   string
}

// mountLog returns the mocked mount calls with jail paths normalised to absolute ones without
// trailing slashes and with the jail's /lib symlink folded into usr/lib, so assertions do not
// depend on how the script spells a path.
func (r cjRunResult) mountLog(t *testing.T) []string {
	return cjNormalizeJailPaths(cjReadLines(t, filepath.Join(r.logDir, "mount.log")))
}

func cjNormalizeJailPaths(lines []string) []string {
	var normalized []string
	for _, line := range lines {
		var args []string
		fields := strings.Fields(line)
		for i, arg := range fields {
			isPath := !strings.HasPrefix(arg, "-") && (i == 0 || fields[i-1] != "-t")
			if isPath && !strings.HasPrefix(arg, "/") {
				arg = "/jail/" + arg
			}
			arg = strings.TrimSuffix(arg, "/")
			args = append(args, strings.Replace(arg, "/jail/lib/", "/jail/usr/lib/", 1))
		}
		normalized = append(normalized, strings.Join(args, " "))
	}
	return normalized
}

func (r cjRunResult) toolLog(t *testing.T, tool string) []string {
	return cjReadLines(t, filepath.Join(r.logDir, tool+".log"))
}

func cjUnsharePrefix(t *testing.T) []string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("complement_jail.sh tests need Linux mount namespaces")
	}
	for _, tool := range []string{"unshare", "chroot", "flock", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found in PATH", tool)
		}
	}
	var prefix []string
	if os.Geteuid() == 0 {
		prefix = []string{"unshare", "-m", "--propagation", "private"}
	} else {
		prefix = []string{"unshare", "-Urm", "--propagation", "private"}
	}
	probe := exec.Command(prefix[0], append(prefix[1:], "true")...)
	if out, err := probe.CombinedOutput(); err != nil {
		msg := fmt.Sprintf("cannot create a private mount namespace (%v: %s); run as root or enable unprivileged user namespaces", err, strings.TrimSpace(string(out)))
		if os.Getenv("COMPLEMENT_JAIL_TEST_REQUIRE") != "" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
	return prefix
}

func newCJSandbox(t *testing.T) *cjSandbox {
	t.Helper()
	sb := &cjSandbox{t: t, unshare: cjUnsharePrefix(t), dir: t.TempDir()}
	sb.root = filepath.Join(sb.dir, "root")
	sb.jail = filepath.Join(sb.dir, "jail")
	sb.upper = filepath.Join(sb.dir, "upper")
	sb.mock = filepath.Join(sb.dir, "mock")
	sb.mockHost = filepath.Join(sb.dir, "mockhost")
	sb.usrLocal = filepath.Join(sb.dir, "usrlocal")

	for _, d := range []string{
		"usr", "proc", "dev", "etc/alternatives", "etc/docker", "tmp", "run/mount", "root",
		"opt/bin/slurm", "var/log", "var/spool/slurmd", "jail", "upper", "mock", "mockhost",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(sb.root, d), 0o755))
	}
	require.NoError(t, os.Chmod(filepath.Join(sb.root, "tmp"), 0o1777))
	// Replicate the host's merged-/usr layout; non-merged hosts get their top-level dirs bind-mounted
	for _, name := range []string{"bin", "sbin", "lib", "lib32", "lib64", "libx32"} {
		if target, err := os.Readlink("/" + name); err == nil {
			require.NoError(t, os.Symlink(target, filepath.Join(sb.root, name)))
		} else if info, err := os.Stat("/" + name); err == nil && info.IsDir() {
			require.NoError(t, os.Mkdir(filepath.Join(sb.root, name), 0o755))
			sb.extraBinds = append(sb.extraBinds, name)
		}
	}
	for _, name := range []string{"ld.so.cache", "passwd", "group", "nsswitch.conf", "hosts", "resolv.conf"} {
		if data, err := os.ReadFile("/etc/" + name); err == nil {
			require.NoError(t, os.WriteFile(filepath.Join(sb.root, "etc", name), data, 0o644))
		}
	}
	sb.writeHostFile(filepath.Join(sb.root, "etc/docker/daemon.json"), `{"default-runtime":"nvidia","runtimes":{"nvidia":{"path":"nvidia-container-runtime"}}}`, 0o644)

	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	script, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), "complement_jail.sh"))
	require.NoError(t, err)
	sb.writeHostFile(filepath.Join(sb.root, "opt/bin/slurm/complement_jail.sh"), string(script), 0o755)
	sb.writeHostFile(filepath.Join(sb.root, "opt/bin/slurm/bind_slurm_common.sh"), "#!/bin/sh\necho \"$*\" >> \"$MOCK_LOG_DIR/bind_slurm_common.log\"\n", 0o755)

	sb.writeMocks()
	sb.writeDriverFiles()
	sb.writeJail()
	return sb
}

func (sb *cjSandbox) writeHostFile(path, contents string, mode os.FileMode) {
	sb.t.Helper()
	require.NoError(sb.t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(sb.t, os.WriteFile(path, []byte(contents), mode))
	require.NoError(sb.t, os.Chmod(path, mode))
}

func (sb *cjSandbox) writeMocks() {
	bin := filepath.Join(sb.mock, "bin")
	require.NoError(sb.t, os.MkdirAll(filepath.Join(sb.mock, "nvidia"), 0o755))
	require.NoError(sb.t, os.MkdirAll(filepath.Join(sb.mock, "log"), 0o755))
	mocks := map[string]string{
		"uname":  "#!/bin/sh\necho " + cjArch + "\n",
		"sudo":   "#!/bin/sh\nexec \"$@\"\n",
		"getcap": "#!/bin/sh\nexit 0\n",
		"setcap": "#!/bin/sh\necho \"$*\" >> \"$MOCK_LOG_DIR/setcap.log\"\n",
		"sleep":  "#!/bin/sh\necho \"$*\" >> \"$MOCK_LOG_DIR/sleep.log\"\nexec /usr/bin/sleep 0.05\n",
		// Logs every call; binds for real when the source exists in the mock host tree (libdummy)
		"mount": `#!/bin/bash
echo "$*" >> "$MOCK_LOG_DIR/mount.log"
if [ "$1" = "--bind" ] && [ -e "${MOCK_HOST}$2" ]; then
    exec /usr/bin/mount --bind "${MOCK_HOST}$2" "$3"
fi
`,
		"findmnt": `#!/bin/bash
echo "$*" >> "$MOCK_LOG_DIR/findmnt.log"
case " $* " in
    *" --pairs "*) cat "$MOCK_DIR/findmnt.pairs" 2>/dev/null || true ;;
    *) echo "fakefs nfs4 rw" ;;
esac
`,
		// list prints the configured host paths; configure bind-mounts them where the real CLI would,
		// creating missing 0-byte targets like the real one. Sources missing in the sandbox come
		// from the mock host tree.
		"nvidia-container-cli": `#!/bin/bash
set -u
echo "$*" >> "$MOCK_LOG_DIR/nvidia-container-cli.log"
if [ "$1" = "list" ]; then
    [ -e "$MOCK_DIR/nvidia/list.fail" ] && exit 1
    cat "$MOCK_DIR/nvidia/list"
    exit 0
fi
jail="${@: -1}"
[ -e "$MOCK_DIR/nvidia/configure.fail" ] && exit 1
[ -e "$MOCK_DIR/nvidia/configure.nomount" ] && exit 0
while IFS= read -r p; do
    [ -n "$p" ] || continue
    src="$p"; [ -e "$src" ] || src="$MOCK_HOST$p"
    case "${p##*/}" in
        lib*.so*) dst="$jail/` + cjLibDir + `/${p##*/}" ;;
        *) dst=$(cd "$jail" && realpath -m ".$p") ;;
    esac
    mkdir -p "${dst%/*}"
    [ -e "$dst" ] || : > "$dst"
    /usr/bin/mount --bind "$src" "$dst"
done < "$MOCK_DIR/nvidia/list"
`,
		// Runs the mocked ldconfig/dpkg-divert against the jail; other commands get absolute args prefixed
		"chroot": `#!/bin/bash
export JAIL="$1"; shift
cmd="$1"; shift
case "$cmd" in
    /usr/sbin/ldconfig) exec "$MOCK_DIR/bin/ldconfig" "$@" ;;
    /usr/bin/dpkg-divert) exec "$MOCK_DIR/bin/dpkg-divert" "$@" ;;
esac
args=()
for a in "$@"; do case "$a" in /*) args+=("$JAIL$a") ;; *) args+=("$a") ;; esac; done
exec "$cmd" "${args[@]}"
`,
		// Models ldconfig on the jail: regular files above 1024 bytes in the flat lib dir are libraries,
		// the soname is derived from the file name (libfoo.so.1.2 -> libfoo.so.1), the highest version
		// wins, the cache is a text file of "=>" lines and soname symlinks are created unless -X.
		"ldconfig": `#!/bin/bash
set -euo pipefail
echo "$*" >> "$MOCK_LOG_DIR/ldconfig.log"
cache=/etc/ld.so.cache; print=0; nolinks=0
while [ $# -gt 0 ]; do
    case "$1" in -p) print=1 ;; -X) nolinks=1 ;; -C) cache=$2; shift ;; esac
    shift
done
if [ "$print" = 0 ] && [ "$cache" = /etc/ld.so.cache ] && [ -e "$MOCK_DIR/ldconfig.fail" ]; then exit 3; fi
cache="$JAIL$cache"
if [ "$print" = 1 ]; then
    n=0; [ -f "$cache" ] && n=$(grep -c . "$cache" || true)
    echo "$n libs found in cache ${cache#"$JAIL"}"
    [ -f "$cache" ] && cat "$cache"
    exit 0
fi
libdir="$JAIL/` + cjLibDir + `"
declare -A best
for f in "$libdir"/lib*.so.*; do
    [ -f "$f" ] && [ ! -L "$f" ] || continue
    [ "$(stat -c %s "$f")" -gt 1024 ] || continue
    name=${f##*/}; soname="${name%%.so.*}.so.1"
    case "${name#*.so.}" in *.*) ;; *) soname=$name ;; esac
    if [ -z "${best[$soname]:-}" ] || [ "$(printf '%s\n%s\n' "${best[$soname]}" "$name" | sort -V | tail -1)" = "$name" ]; then
        best[$soname]=$name
    fi
done
: > "$cache.tmp"
for soname in "${!best[@]}"; do
    printf '\t%s (libc6,x86-64) => /%s/%s\n' "$soname" "` + cjLibDir + `" "$soname" >> "$cache.tmp"
done
sort -o "$cache" "$cache.tmp"; rm -f "$cache.tmp"
[ "$nolinks" = 0 ] || exit 0
if [ -e "$MOCK_DIR/ldconfig.nolink_once" ]; then rm -f "$MOCK_DIR/ldconfig.nolink_once"; exit 0; fi
for soname in "${!best[@]}"; do [ "$soname" = "${best[$soname]}" ] || ln -sfn "${best[$soname]}" "$libdir/$soname"; done
`,
		// Appends to the jail's diversions file in dpkg's three-line format; identical entries are left alone
		"dpkg-divert": `#!/bin/bash
set -euo pipefail
echo "$*" >> "$MOCK_LOG_DIR/dpkg-divert.log"
[ -e "$MOCK_DIR/divert.fail" ] && exit 2
from=""; to=""
while [ $# -gt 0 ]; do
    case "$1" in --divert) to=$2; shift ;; --add) from=$2; shift ;; esac
    shift
done
db="$JAIL/var/lib/dpkg/diversions"; mkdir -p "${db%/*}"; touch "$db"
if awk -v f="$from" -v t="$to" 'NR%3==1 {a=$0} NR%3==2 {if (a==f && $0==t) found=1} END {exit !found}' "$db"; then
    echo "Leaving 'local diversion of $from to $to'"; exit 0
fi
printf '%s\n%s\n:\n' "$from" "$to" >> "$db"
echo "Adding 'local diversion of $from to $to'"
`,
	}
	for name, contents := range mocks {
		sb.writeHostFile(filepath.Join(bin, name), contents, 0o755)
	}
	sb.setFindmntPairs(`TARGET="/upper/home"` + "\n")
	require.NoError(sb.t, os.MkdirAll(filepath.Join(sb.upper, "home"), 0o755))
}

func (sb *cjSandbox) writeDriverFiles() {
	var list strings.Builder
	for _, f := range cjDriverFiles {
		path := filepath.Join(sb.mockHost, f.hostPath)
		if strings.HasPrefix(f.hostPath, "/usr/local/") {
			path = filepath.Join(sb.usrLocal, strings.TrimPrefix(f.hostPath, "/usr/local/"))
		}
		sb.writeHostFile(path, cjLibContent(f.hostPath), f.mode)
		list.WriteString(f.hostPath + "\n")
	}
	sb.writeHostFile(filepath.Join(sb.mockHost, "lib", cjArch+"-linux-gnu", "libdummy.so"), cjLibContent("libdummy"), 0o644)
	sb.writeHostFile(filepath.Join(sb.mock, "nvidia/list"), list.String(), 0o644)
}

func (sb *cjSandbox) writeJail() {
	for _, d := range []string{
		"proc", "sys/fs/cgroup", "dev", "run", "tmp", "var/log", "var/spool/slurmd", "var/lib/dpkg",
		"etc", "usr/bin", "usr/sbin", cjLibDir, "opt",
	} {
		require.NoError(sb.t, os.MkdirAll(filepath.Join(sb.jail, d), 0o755))
	}
	require.NoError(sb.t, os.Symlink("usr/lib", filepath.Join(sb.jail, "lib")))
	for _, f := range []string{"etc/resolv.conf", "etc/hosts", "usr/bin/docker", "usr/bin/dockerd"} {
		sb.writeHostFile(filepath.Join(sb.jail, f), "", 0o644)
	}
	// A real jail always has libraries and a non-empty linker cache; the fake ldconfig's cache format
	sb.writeHostFile(filepath.Join(sb.jail, cjLibDir, "libc.so.6"), cjLibContent("libc"), 0o644)
	sb.writeHostFile(filepath.Join(sb.jail, "etc/ld.so.cache"), "\tlibc.so.6 (libc6,x86-64) => /"+cjLibDir+"/libc.so.6\n", 0o644)
	// Small non-library files the libdummy loop must leave alone
	sb.writeHostFile(filepath.Join(sb.jail, cjLibDir, "libc_nonshared.a"), strings.Repeat("a", 100), 0o644)
	sb.writeHostFile(filepath.Join(sb.jail, cjLibDir, "libncurses.so"), "INPUT(libncurses.so.6 -ltinfo)\n", 0o644)
}

func (sb *cjSandbox) setFindmntPairs(contents string) {
	sb.writeHostFile(filepath.Join(sb.mock, "findmnt.pairs"), contents, 0o644)
}

func (sb *cjSandbox) setMockFlag(name string) {
	sb.writeHostFile(filepath.Join(sb.mock, name), "", 0o644)
}

func (sb *cjSandbox) jailPath(rel string) string { return filepath.Join(sb.jail, rel) }

// run executes the script once in a fresh sandbox namespace. Safe for concurrent use.
func (sb *cjSandbox) run(opts cjRunOpts) cjRunResult {
	t := sb.t
	id := sb.runs.Add(1)
	logDir := filepath.Join(sb.mock, "log", fmt.Sprintf("run-%d", id))
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	var binds strings.Builder
	bind := func(flag, src, dst string) {
		fmt.Fprintf(&binds, "mount %s %s %s\n", flag, cjQuote(src), cjQuote(filepath.Join(sb.root, dst)))
	}
	bind("--bind", "/usr", "usr")
	bind("--bind", sb.usrLocal, "usr/local")
	bind("--rbind", "/proc", "proc")
	bind("--rbind", "/dev", "dev")
	if _, err := os.Stat("/etc/alternatives"); err == nil {
		bind("--bind", "/etc/alternatives", "etc/alternatives")
	}
	for _, name := range sb.extraBinds {
		bind("--bind", "/"+name, name)
	}
	bind("--bind", sb.jail, "jail")
	bind("--bind", sb.upper, "upper")
	bind("--bind", sb.mock, "mock")
	bind("--bind", sb.mockHost, "mockhost")

	env := []string{
		"PATH=/mock/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root", "MOCK_DIR=/mock", "MOCK_HOST=/mockhost", "PYTHONDONTWRITEBYTECODE=1",
		"MOCK_LOG_DIR=/mock/log/" + filepath.Base(logDir),
	}
	keys := make([]string, 0, len(opts.env))
	for k := range opts.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+opts.env[k])
	}
	var quotedEnv []string
	for _, e := range env {
		quotedEnv = append(quotedEnv, cjQuote(e))
	}
	args := "-j /jail -u /upper"
	if opts.worker {
		args += " -w"
	}
	launcher := "#!/bin/bash\nset -euo pipefail\n" + binds.String() +
		"exec chroot " + cjQuote(sb.root) + " /usr/bin/env -i " + strings.Join(quotedEnv, " ") +
		" /bin/bash /opt/bin/slurm/complement_jail.sh " + args + "\n"
	launcherPath := filepath.Join(logDir, "launch.sh")
	require.NoError(t, os.WriteFile(launcherPath, []byte(launcher), 0o755))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, sb.unshare[0], append(append([]string{}, sb.unshare[1:]...), "bash", launcherPath)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = cmd.Stdout
	require.NoError(t, cmd.Start())

	var output strings.Builder
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	for scanner.Scan() {
		line := scanner.Text()
		output.WriteString(line)
		output.WriteByte('\n')
		if opts.onLine != nil {
			opts.onLine(line)
		}
	}
	_, _ = io.Copy(io.Discard, stdout)
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "script did not exit normally: %v\n%s", err, output.String())
		exitCode = exitErr.ExitCode()
	}
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "output.log"), []byte(output.String()), 0o644))
	return cjRunResult{output: output.String(), exitCode: exitCode, logDir: logDir}
}

func cjQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func cjLibContent(seed string) string {
	return strings.Repeat(seed+"\n", 1+4096/(len(seed)+1))
}

func cjReadLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// cjCountMessages counts output lines ending with msg: set -x traces quote the message, so they do not match
func cjCountMessages(output, msg string) int {
	n := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.HasSuffix(line, msg) {
			n++
		}
	}
	return n
}

// cjRequireInOrder checks that every expected line is present and that they appear in the given order
func cjRequireInOrder(t *testing.T, lines []string, expected ...string) {
	t.Helper()
	last := -1
	for _, want := range expected {
		found := -1
		for i := last + 1; i < len(lines); i++ {
			if lines[i] == want {
				found = i
				break
			}
		}
		require.NotEqual(t, -1, found, "line %q missing or out of order in:\n%s", want, strings.Join(lines, "\n"))
		last = found
	}
}

func cjCountPrefix(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

// cjReadDiversions returns "from -> to" entries of a dpkg diversions file
func cjReadDiversions(t *testing.T, path string) []string {
	t.Helper()
	lines := cjReadLines(t, path)
	var entries []string
	for i := 0; i+2 < len(lines); i += 3 {
		entries = append(entries, lines[i]+" -> "+lines[i+1])
	}
	return entries
}

func cjExpectedDiversions() []string {
	flat := "/" + cjLibDir
	divert := func(path string) string {
		return path + " -> " + filepath.Dir(path) + "/" + cjDivertSubdir + "/" + filepath.Base(path)
	}
	var want []string
	for _, lib := range cjFlatLibs {
		want = append(want, divert(flat+"/"+lib))
	}
	want = append(want,
		divert("/usr/local/bin/nvidia-smi"),
		divert("/usr/lib/firmware/nvidia/"+cjDriverVersion+"/gsp_ga10x.bin"),
		divert("/lib/firmware/nvidia/"+cjDriverVersion+"/gsp_ga10x.bin"),
		divert(flat+"/libcuda.so.1"),
		divert(flat+"/libnvidia-ml.so.1"),
		divert(flat+"/libvdpau_nvidia.so.1"),
	)
	return want
}

var cjGPUWorkerEnv = map[string]string{"NODESET_GPU_ENABLED": "true", "SLURM_CLUSTER_WITH_GPU": "true"}

func cjRequireGPUJailProvisioned(t *testing.T, sb *cjSandbox) {
	t.Helper()
	for _, f := range cjDriverFiles {
		var rel string
		switch {
		case strings.HasPrefix(filepath.Base(f.hostPath), "lib"):
			rel = filepath.Join(cjLibDir, filepath.Base(f.hostPath))
		case strings.HasPrefix(f.hostPath, "/lib/"):
			rel = filepath.Join("usr", f.hostPath)
		default:
			rel = f.hostPath
		}
		data, err := os.ReadFile(sb.jailPath(rel))
		require.NoError(t, err, "placeholder %s", rel)
		require.Equal(t, cjMarker, string(data), "placeholder %s content", rel)
		if strings.HasPrefix(f.hostPath, "/usr/local/") {
			info, err := os.Stat(sb.jailPath(rel))
			require.NoError(t, err)
			require.Equal(t, f.mode, info.Mode().Perm(), "placeholder %s copies the host mode", rel)
		}
	}
	for _, lib := range cjFlatLibs {
		soname := lib[:strings.Index(lib, ".so.")] + ".so.1"
		target, err := os.Readlink(sb.jailPath(filepath.Join(cjLibDir, soname)))
		require.NoError(t, err, "soname symlink %s", soname)
		require.Equal(t, lib, target)
	}
	require.FileExists(t, sb.jailPath("etc/gpu_libs_installed.flag"))
	require.Len(t, cjReadLines(t, sb.jailPath("etc/ld.so.cache")), 4, "libc plus three driver sonames")
	require.ElementsMatch(t, cjExpectedDiversions(), cjReadDiversions(t, sb.jailPath("var/lib/dpkg/diversions")))
	require.DirExists(t, sb.jailPath(filepath.Join(cjLibDir, cjDivertSubdir)))
	require.DirExists(t, sb.jailPath("usr/local/bin/"+cjDivertSubdir))
	require.DirExists(t, sb.jailPath("usr/lib/firmware/nvidia/"+cjDriverVersion+"/"+cjDivertSubdir))
}

func TestComplementJailGPUWorkerColdJail(t *testing.T) { testGPUWorkerColdJail(t, newCJSandbox(t)) }

func testGPUWorkerColdJail(t *testing.T, sb *cjSandbox) {
	res := sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv})
	require.Equal(t, 0, res.exitCode, res.output)

	cjRequireGPUJailProvisioned(t, sb)
	for _, msg := range []string{
		"[nvml] 5 GPU placeholders missing, creating under lock",
		"[nvml] pre-created 5 GPU placeholders:",
		"[nvml] GPU libs mounted, 5 new jail mounts verified",
		"[divert] adding 6 dpkg diversions under lock",
		"[ldcache] Linker cache is stale, updating it inside the jail",
		"[ldcache] ldconfig completed successfully (got flock)",
		"[nvml] libnvidia-ml.so.1 usable after ldconfig phase (ran)",
		"[divert] adding 3 dpkg diversions under lock",
	} {
		require.Contains(t, res.output, msg)
	}
	require.NotContains(t, res.output, "ANOMALY")
	require.Len(t, res.toolLog(t, "dpkg-divert"), len(cjExpectedDiversions()))

	cli := res.toolLog(t, "nvidia-container-cli")
	require.Len(t, cli, 2)
	require.Equal(t, "list", cli[0])
	require.Equal(t, "--user --debug=/dev/stderr --no-pivot configure --no-cgroups --cuda-compat-mode=disabled --ldconfig=/usr/bin/true --device=all --compute --utility /jail", cli[1])

	ldconfig := res.toolLog(t, "ldconfig")
	require.Equal(t, []string{"-p", "-X -i -C /tmp/ld.so.cache.candidate." + cjPID(ldconfig[1]), "-C /tmp/ld.so.cache.candidate." + cjPID(ldconfig[1]) + " -p", ""}, ldconfig)

	cjRequireInOrder(t, res.mountLog(t),
		"-t proc /proc /jail/proc",
		"-t sysfs /sys /jail/sys",
		"--rbind /dev /jail/dev",
		"--rbind /run /jail/run",
		"--rbind /sys/fs/cgroup /jail/sys/fs/cgroup",
		"--bind /tmp /jail/tmp",
		"--bind /var/log /jail/var/log",
		"--bind /etc/resolv.conf /jail/etc/resolv.conf",
		"--bind /etc/hosts /jail/etc/hosts",
		"--rbind /upper/home /jail/home",
		"--bind /"+cjLibDir+"/slurm/chroot.so /jail/usr/lib/slurm/chroot.so",
		"--bind /"+cjLibDir+"/slurm/spanknccldebug.so /jail/usr/lib/slurm/spanknccldebug.so",
		"--bind /"+cjLibDir+"/slurm/spank_nccl_inspector_preconf.so /jail/usr/lib/slurm/spank_nccl_inspector_preconf.so",
		"--rbind /etc/enroot /jail/etc/enroot",
		"--bind /usr/share/enroot /jail/usr/share/enroot",
		"--bind /usr/lib/enroot /jail/usr/lib/enroot",
		"--bind /"+cjLibDir+"/slurm/spank_pyxis.so /jail/usr/lib/slurm/spank_pyxis.so",
		"--bind /var/spool/slurmd /jail/var/spool/slurmd",
		"--bind /usr/sbin/slurmd /jail/usr/sbin/slurmd",
		"--bind /usr/sbin/slurmstepd /jail/usr/sbin/slurmstepd",
		"--bind /etc/docker/daemon.json /jail/etc/docker/daemon.json",
	)
	require.Equal(t, 0, cjCountPrefix(res.mountLog(t), "--bind /lib/"+cjArch+"-linux-gnu/libdummy.so"), "GPU workers never mount libdummy")
	require.Equal(t, []string{"-j /jail"}, res.toolLog(t, "bind_slurm_common"))
	var setcaps []string
	for _, line := range res.toolLog(t, "setcap") {
		capabilities, path, _ := strings.Cut(line, " ")
		setcaps = append(setcaps, capabilities+" "+cjNormalizeJailPaths([]string{path})[0])
	}
	require.ElementsMatch(t, []string{
		"cap_sys_admin+pe /jail/usr/bin/enroot-mksquashovlfs",
		"cap_sys_admin,cap_mknod+pe /jail/usr/bin/enroot-aufs2ovlfs",
	}, setcaps)

	fstab, err := os.ReadFile(filepath.Join(sb.root, "etc/enroot/mounts.d/task_prolog.fstab"))
	require.NoError(t, err)
	require.Equal(t, "/opt/slurm_scripts/task_prolog.sh /opt/slurm_scripts/task_prolog.sh none x-create=file,bind,ro,nosuid,nodev,private,nofail 0 0\n", string(fstab))

	for _, dir := range []string{"opt/soperator-outputs", "opt/soperator-outputs/local/nccl_logs", "opt/soperator-outputs/shared/acceptance"} {
		info, err := os.Stat(sb.jailPath(dir))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o777), info.Mode().Perm(), dir)
	}
	for _, dir := range []string{"var/cache/enroot-container-images", "var/cache/enroot"} {
		info, err := os.Stat(sb.jailPath(dir))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o777), info.Mode().Perm(), dir)
		require.NotZero(t, info.Mode()&os.ModeSticky, dir)
	}
	require.DirExists(t, sb.jailPath("home"))
}

// cjPID extracts the trailing PID from an ldconfig candidate-cache argument
func cjPID(arg string) string { return arg[strings.LastIndex(arg, ".")+1:] }

func TestComplementJailGPUWorkerWarmJailIsReadOnly(t *testing.T) {
	testGPUWorkerWarmJailIsReadOnly(t, newCJSandbox(t))
}

func testGPUWorkerWarmJailIsReadOnly(t *testing.T, sb *cjSandbox) {
	require.Equal(t, 0, sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv}).exitCode)

	stat := func(rel string) time.Time {
		info, err := os.Lstat(sb.jailPath(rel))
		require.NoError(t, err)
		return info.ModTime()
	}
	before := map[string]time.Time{}
	for _, rel := range []string{filepath.Join(cjLibDir, cjFlatLibs[0]), filepath.Join(cjLibDir, "libcuda.so.1"), "etc/ld.so.cache", "var/lib/dpkg/diversions"} {
		before[rel] = stat(rel)
	}
	time.Sleep(20 * time.Millisecond)

	res := sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv})
	require.Equal(t, 0, res.exitCode, res.output)
	cjRequireGPUJailProvisioned(t, sb)
	for _, msg := range []string{
		"[nvml] all GPU placeholders already present in the jail",
		"[nvml] GPU libs mounted, 5 new jail mounts verified",
		"[ldcache] Linker cache is already up to date, skipping ldconfig",
		"[nvml] libnvidia-ml.so.1 usable after ldconfig phase (skipped)",
	} {
		require.Contains(t, res.output, msg)
	}
	require.Equal(t, 2, cjCountMessages(res.output, "[divert] all dpkg diversions present"))
	require.NotContains(t, res.output, "ANOMALY")
	require.Empty(t, res.toolLog(t, "dpkg-divert"))
	ldconfig := res.toolLog(t, "ldconfig")
	candidate := "/tmp/ld.so.cache.candidate." + cjPID(ldconfig[1])
	require.Equal(t, []string{"-p", "-X -i -C " + candidate, "-C " + candidate + " -p"}, ldconfig, "only the probe, no cache rebuild")
	for rel, mtime := range before {
		require.Equal(t, mtime, stat(rel), "%s was rewritten on a warm jail", rel)
	}
}

func TestComplementJailDriverCapabilities(t *testing.T) { testDriverCapabilities(t, newCJSandbox(t)) }

func testDriverCapabilities(t *testing.T, sb *cjSandbox) {

	env := map[string]string{"NODESET_GPU_ENABLED": "true", "SLURM_CLUSTER_WITH_GPU": "true", "NVIDIA_DRIVER_CAPABILITIES": "all"}
	res := sb.run(cjRunOpts{worker: true, env: env})
	require.Equal(t, 0, res.exitCode, res.output)
	require.Contains(t, res.toolLog(t, "nvidia-container-cli")[1], " --device=all --compute --compat32 --display --graphics --utility --video /jail")

	env["NVIDIA_DRIVER_CAPABILITIES"] = "compute,bogus"
	res = sb.run(cjRunOpts{worker: true, env: env})
	require.Equal(t, 1, res.exitCode)
	require.Contains(t, res.output, "Unknown NVIDIA driver capability: bogus")
	require.Empty(t, res.toolLog(t, "nvidia-container-cli"))
}

func TestComplementJailCLIListFailure(t *testing.T) { testCLIListFailure(t, newCJSandbox(t)) }

func testCLIListFailure(t *testing.T, sb *cjSandbox) {
	sb.setMockFlag("nvidia/list.fail")
	res := sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv})
	require.Equal(t, 1, res.exitCode)
	require.Contains(t, res.output, "nvidia-container-cli list failed, exiting so the container restarts")
	require.NoFileExists(t, sb.jailPath(filepath.Join(cjLibDir, cjFlatLibs[0])))
	require.NoFileExists(t, sb.jailPath("etc/gpu_libs_installed.flag"))
}

func TestComplementJailCLIConfigureFailure(t *testing.T) { testCLIConfigureFailure(t, newCJSandbox(t)) }

func testCLIConfigureFailure(t *testing.T, sb *cjSandbox) {
	sb.setMockFlag("nvidia/configure.fail")
	res := sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv})
	require.Equal(t, 1, res.exitCode)
	require.Contains(t, res.output, "[nvml] nvidia-container-cli failed, exiting so the container restarts")
	require.Contains(t, res.output, "[nvml] === NVML jail state: after failed nvidia-container-cli ===")
	// Placeholders are created before the CLI runs and survive its failure
	data, err := os.ReadFile(sb.jailPath(filepath.Join(cjLibDir, cjFlatLibs[0])))
	require.NoError(t, err)
	require.Equal(t, cjMarker, string(data))
	require.NoFileExists(t, sb.jailPath("etc/gpu_libs_installed.flag"))
	require.Empty(t, res.toolLog(t, "ldconfig"))
}

func TestComplementJailCLIWithoutMounts(t *testing.T) { testCLIWithoutMounts(t, newCJSandbox(t)) }

func testCLIWithoutMounts(t *testing.T, sb *cjSandbox) {
	sb.setMockFlag("nvidia/configure.nomount")
	res := sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv})
	require.Equal(t, 1, res.exitCode)
	require.Contains(t, res.output, "[nvml] ANOMALY: no new jail mounts after nvidia-container-cli, exiting so the container restarts")
}

func TestComplementJailMountedLibWithoutContent(t *testing.T) {
	testMountedLibWithoutContent(t, newCJSandbox(t))
}

func testMountedLibWithoutContent(t *testing.T, sb *cjSandbox) {
	sb.writeHostFile(filepath.Join(sb.usrLocal, "lib", cjArch+"-linux-gnu", "libnvidia-ml.so."+cjDriverVersion), "tiny", 0o644)
	res := sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv})
	require.Equal(t, 1, res.exitCode)
	require.Contains(t, res.output, "[nvml] ANOMALY: /jail/"+cjLibDir+"/libnvidia-ml.so."+cjDriverVersion+" shows no real content right after mounting, exiting so the container restarts")
}

func TestComplementJailLdconfigRepairsMissingSoname(t *testing.T) {
	testLdconfigRepairsMissingSoname(t, newCJSandbox(t))
}

func testLdconfigRepairsMissingSoname(t *testing.T, sb *cjSandbox) {
	sb.setMockFlag("ldconfig.nolink_once")
	res := sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv})
	require.Equal(t, 0, res.exitCode, res.output)
	require.Contains(t, res.output, "[nvml] ANOMALY: libnvidia-ml.so.1 missing or empty after ldconfig phase (ran), running ldconfig here")
	require.Contains(t, res.output, "[nvml] libnvidia-ml.so.1 repaired by local ldconfig")
	ldconfig := res.toolLog(t, "ldconfig")
	require.Equal(t, "", ldconfig[len(ldconfig)-1], "repair runs a plain ldconfig")
	require.Equal(t, "", ldconfig[len(ldconfig)-2], "the regular ldconfig ran before the repair")
	cjRequireGPUJailProvisioned(t, sb)
}

func TestComplementJailLdconfigFailure(t *testing.T) { testLdconfigFailure(t, newCJSandbox(t)) }

func testLdconfigFailure(t *testing.T, sb *cjSandbox) {
	sb.setMockFlag("ldconfig.fail")
	res := sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv})
	require.Equal(t, 1, res.exitCode, "the soname check exits before the ldconfig exit code propagates")
	require.Contains(t, res.output, "[ldcache] ldconfig FAILED with exit code 3")
	require.Contains(t, res.output, "[nvml] ANOMALY: libnvidia-ml.so.1 missing or empty after ldconfig phase (failed), running ldconfig here")
	require.Contains(t, res.output, "[nvml] libnvidia-ml.so.1 still missing or empty after repair, exiting so the container restarts")
	require.NoFileExists(t, sb.jailPath(filepath.Join(cjLibDir, "libnvidia-ml.so.1")))
}

func TestComplementJailLdconfigLockHeldByAnotherWorker(t *testing.T) {
	testLdconfigLockHeldByAnotherWorker(t, newCJSandbox(t))
}

func testLdconfigLockHeldByAnotherWorker(t *testing.T, sb *cjSandbox) {
	lockPath := sb.jailPath("etc/complement_jail_ldconfig.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err)
	defer lock.Close()
	require.NoError(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_EX))

	var unlockOnce sync.Once
	res := sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv, onLine: func(line string) {
		if strings.Contains(line, "another worker holds the lock") {
			unlockOnce.Do(func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) })
		}
	}})
	require.Equal(t, 0, res.exitCode, res.output)
	require.Contains(t, res.output, "[ldcache] ldconfig skipped: another worker holds the lock and is rebuilding the cache")
	// The lock holder never produced the symlink here, so this worker repairs after the grace waits
	require.Contains(t, res.output, "[nvml] ANOMALY: libnvidia-ml.so.1 missing or empty after ldconfig phase (lock-busy), running ldconfig here")
	require.Contains(t, res.output, "[nvml] libnvidia-ml.so.1 repaired by local ldconfig")
	cjRequireGPUJailProvisioned(t, sb)
}

// Anchored at the line start (after an optional timestamp) so the shell's xtrace echo of the same text does not count
var cjPlaceholdersCreated = regexp.MustCompile(`(?m)^(?:\[\d\d:\d\d:\d\d\] )?\[nvml\] pre-created (\d+) GPU placeholders:`)

func TestComplementJailConcurrentGPUWorkers(t *testing.T) {
	testConcurrentGPUWorkers(t, newCJSandbox(t))
}

func testConcurrentGPUWorkers(t *testing.T, sb *cjSandbox) {
	const workers = 3
	results := make([]cjRunResult, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv})
		}()
	}
	wg.Wait()

	created, ran := 0, 0
	for i, res := range results {
		require.Equal(t, 0, res.exitCode, "worker %d:\n%s", i, res.output)
		require.NotContains(t, res.output, "ANOMALY", "worker %d", i)
		require.Contains(t, res.output, "[nvml] libnvidia-ml.so.1 usable after ldconfig phase")
		for _, match := range cjPlaceholdersCreated.FindAllStringSubmatch(res.output, -1) {
			n, err := strconv.Atoi(match[1])
			require.NoError(t, err)
			created += n
		}
		ran += cjCountMessages(res.output, "[ldcache] ldconfig completed successfully (got flock)")
	}
	require.Equal(t, 5, created, "placeholders are created exactly once across workers")
	diverted := 0
	for _, res := range results {
		diverted += len(res.toolLog(t, "dpkg-divert"))
	}
	require.Equal(t, len(cjExpectedDiversions()), diverted, "each diversion is added exactly once across workers")
	require.GreaterOrEqual(t, ran, 1)
	cjRequireGPUJailProvisioned(t, sb)
}

func TestComplementJailDiversionsSkipWhileDpkgIsBusy(t *testing.T) {
	testDiversionsSkipWhileDpkgIsBusy(t, newCJSandbox(t))
}

func testDiversionsSkipWhileDpkgIsBusy(t *testing.T, sb *cjSandbox) {
	// apt and dpkg hold POSIX write locks on these files; one is enough to signal a running operation
	require.NoError(t, os.MkdirAll(sb.jailPath("var/lib/dpkg"), 0o755))
	lock, err := os.OpenFile(sb.jailPath("var/lib/dpkg/lock-frontend"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err)
	require.NoError(t, syscall.FcntlFlock(lock.Fd(), syscall.F_SETLK, &syscall.Flock_t{Type: syscall.F_WRLCK, Whence: int16(io.SeekStart)}))

	res := sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv})
	require.Equal(t, 0, res.exitCode, res.output)
	require.Equal(t, 2, cjCountMessages(res.output, "[divert] dpkg database is in use (a package operation is running), skipping until the next start"))
	require.Empty(t, res.toolLog(t, "dpkg-divert"))
	require.NoFileExists(t, sb.jailPath("var/lib/dpkg/diversions"))

	require.NoError(t, lock.Close())
	res = sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv})
	require.Equal(t, 0, res.exitCode, res.output)
	cjRequireGPUJailProvisioned(t, sb)
}

func TestComplementJailLoginNodeInGPUCluster(t *testing.T) {
	testLoginNodeInGPUCluster(t, newCJSandbox(t))
}

func testLoginNodeInGPUCluster(t *testing.T, sb *cjSandbox) {
	require.Equal(t, 0, sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv}).exitCode)
	// A later driver placeholder nobody mounts on this node must still be covered by libdummy
	sb.writeHostFile(sb.jailPath(filepath.Join(cjLibDir, "libnvidia-cfg.so.1.2.3")), "", 0o644)

	env := map[string]string{"SLURM_CLUSTER_WITH_GPU": "true"}
	res := sb.run(cjRunOpts{env: env})
	require.Equal(t, 0, res.exitCode, res.output)
	require.NotContains(t, res.output, "[gpu-wait] Waiting for GPU libs")

	mounts := res.mountLog(t)
	var dummy []string
	for _, m := range mounts {
		if strings.HasPrefix(m, "--bind /lib/"+cjArch+"-linux-gnu/libdummy.so ") {
			dummy = append(dummy, strings.TrimPrefix(m, "--bind /lib/"+cjArch+"-linux-gnu/libdummy.so /jail/"+cjLibDir+"/"))
		}
	}
	require.ElementsMatch(t, append(cjFlatLibs, "libnvidia-cfg.so.1.2.3"), dummy)
	require.Equal(t, 0, cjCountPrefix(mounts, "--bind /usr/sbin/slurmd"), "login nodes get no slurmd")
	require.Equal(t, 0, cjCountPrefix(mounts, "--bind /var/spool/slurmd"))
	require.Empty(t, res.toolLog(t, "nvidia-container-cli"))
	require.Empty(t, res.toolLog(t, "ldconfig"))
	require.Empty(t, res.toolLog(t, "dpkg-divert"))
}

func TestComplementJailLoginNodeWaitsForGPULibs(t *testing.T) {
	testLoginNodeWaitsForGPULibs(t, newCJSandbox(t))
}

func testLoginNodeWaitsForGPULibs(t *testing.T, sb *cjSandbox) {
	var once sync.Once
	res := sb.run(cjRunOpts{env: map[string]string{"SLURM_CLUSTER_WITH_GPU": "true"}, onLine: func(line string) {
		if strings.Contains(line, "[gpu-wait] Waiting for GPU libs to be propagated to the jail from a worker node") {
			once.Do(func() {
				sb.writeHostFile(sb.jailPath(filepath.Join(cjLibDir, cjFlatLibs[0])), cjMarker, 0o644)
				sb.writeHostFile(sb.jailPath("etc/gpu_libs_installed.flag"), "", 0o644)
			})
		}
	}})
	require.Equal(t, 0, res.exitCode, res.output)
	require.Contains(t, res.output, "[gpu-wait] Waiting for GPU libs")
	require.Contains(t, res.mountLog(t), "--bind /lib/"+cjArch+"-linux-gnu/libdummy.so /jail/"+cjLibDir+"/"+cjFlatLibs[0])
}

func TestComplementJailCPUWorkerInGPUCluster(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found in PATH")
	}
	testCPUWorkerInGPUCluster(t, newCJSandbox(t))
}

func testCPUWorkerInGPUCluster(t *testing.T, sb *cjSandbox) {
	require.Equal(t, 0, sb.run(cjRunOpts{worker: true, env: cjGPUWorkerEnv}).exitCode)

	env := map[string]string{"SLURM_CLUSTER_WITH_GPU": "true", "NODESET_GPU_ENABLED": "false"}
	res := sb.run(cjRunOpts{worker: true, env: env})
	require.Equal(t, 0, res.exitCode, res.output)
	require.NotContains(t, res.output, "ANOMALY")

	mounts := res.mountLog(t)
	require.Equal(t, len(cjFlatLibs), cjCountPrefix(mounts, "--bind /lib/"+cjArch+"-linux-gnu/libdummy.so "))
	cjRequireInOrder(t, mounts,
		"--bind /var/spool/slurmd /jail/var/spool/slurmd",
		"--bind /usr/sbin/slurmd /jail/usr/sbin/slurmd",
		"--bind /run/soperator-docker-daemon.json /jail/etc/docker/daemon.json",
	)
	config, err := os.ReadFile(filepath.Join(sb.root, "run/soperator-docker-daemon.json"))
	require.NoError(t, err)
	require.Contains(t, string(config), `"default-runtime": "runc"`)
	require.Contains(t, string(config), `"nvidia-container-runtime"`, "other settings are preserved")

	require.Empty(t, res.toolLog(t, "nvidia-container-cli"))
	require.NotContains(t, res.output, "[ldcache]", "CPU workers in GPU clusters leave the shared linker cache to GPU workers")
	require.Empty(t, res.toolLog(t, "ldconfig"))
	require.Empty(t, res.toolLog(t, "dpkg-divert"), "CPU workers do not touch diversions")
}

func TestComplementJailWorkerWithDockerDisabled(t *testing.T) {
	testWorkerWithDockerDisabled(t, newCJSandbox(t))
}

func testWorkerWithDockerDisabled(t *testing.T, sb *cjSandbox) {
	res := sb.run(cjRunOpts{worker: true, env: map[string]string{"SOPERATOR_DOCKER_ENABLED": "false"}})
	require.Equal(t, 0, res.exitCode, res.output)

	stub, err := os.ReadFile(filepath.Join(sb.root, "opt/soperator-docker-disabled.sh"))
	require.NoError(t, err)
	require.Contains(t, string(stub), "docker is not available on this cluster")
	mounts := res.mountLog(t)
	cjRequireInOrder(t, mounts,
		"--bind /opt/soperator-docker-disabled.sh /jail/usr/bin/docker",
		"--bind /opt/soperator-docker-disabled.sh /jail/usr/bin/dockerd",
	)
	require.Equal(t, 0, cjCountPrefix(mounts, "--bind /etc/docker/daemon.json"))
	require.Equal(t, 0, cjCountPrefix(mounts, "--bind /lib/"+cjArch+"-linux-gnu/libdummy.so"))
	require.Contains(t, res.output, "[ldcache] Linker cache is already up to date, skipping ldconfig")
	require.Empty(t, res.toolLog(t, "nvidia-container-cli"))
	require.NotContains(t, res.output, "[nvml]")
}

func TestComplementJailSubmounts(t *testing.T) { testSubmounts(t, newCJSandbox(t)) }

func testSubmounts(t *testing.T, sb *cjSandbox) {
	sb.setFindmntPairs(strings.Join([]string{
		`TARGET="/upper/home"`,
		`TARGET="/upper/etc/custom.conf"`,
		`TARGET="/upper/missing"`,
		`TARGET="/somewhere/else"`,
	}, "\n") + "\n")
	sb.writeHostFile(filepath.Join(sb.upper, "etc/custom.conf"), "x", 0o644)

	res := sb.run(cjRunOpts{})
	require.Equal(t, 0, res.exitCode, res.output)
	mounts := res.mountLog(t)
	cjRequireInOrder(t, mounts, "--rbind /upper/home /jail/home", "--bind /upper/etc/custom.conf /jail/etc/custom.conf")
	require.Equal(t, 0, cjCountPrefix(mounts, "--rbind /upper/missing"))
	require.Equal(t, 0, cjCountPrefix(mounts, "--bind /somewhere"))
	require.DirExists(t, sb.jailPath("home"))
	require.FileExists(t, sb.jailPath("etc/custom.conf"))
}
