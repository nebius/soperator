#define _GNU_SOURCE

#include <assert.h>
#include <security/pam_appl.h>
#include <sys/wait.h>

#include "../pam_soperator_jail.c"
#include "../chroot-plugin/chroot.c"

static const char *const test_paths[] = {"/mnt/memory", "/dev/shm", "/tmp"};

static void require_success(int result, const char *operation)
{
    if (result != 0) {
        perror(operation);
        exit(EXIT_FAILURE);
    }
}

static void join_path(char result[PATH_MAX], const char *base, const char *suffix)
{
    int written = snprintf(result, PATH_MAX, "%s%s", base, suffix);

    assert(written >= 0 && written < PATH_MAX);
}

static void write_marker(const char *base, const char *name)
{
    char path[PATH_MAX];
    int fd;

    join_path(path, base, name);
    fd = open(path, O_CREAT | O_EXCL | O_WRONLY, 0600);
    require_success(fd < 0 ? -1 : 0, "create marker");
    assert(write(fd, name, strlen(name)) == (ssize_t)strlen(name));
    require_success(close(fd), "close marker");
}

static void assert_marker(const char *base, const char *name, bool expected)
{
    char path[PATH_MAX];
    char content[128];
    int fd;

    join_path(path, base, name);
    fd = open(path, O_RDONLY);
    if (!expected) {
        assert(fd == -1 && errno == ENOENT);
        return;
    }
    require_success(fd < 0 ? -1 : 0, "read marker");
    ssize_t length = read(fd, content, sizeof(content));
    assert(length == (ssize_t)strlen(name));
    assert(memcmp(content, name, (size_t)length) == 0);
    require_success(close(fd), "close marker");
}

static void wait_success(pid_t child)
{
    int status;

    assert(child > 0);
    assert(waitpid(child, &status, 0) == child);
    assert(WIFEXITED(status) && WEXITSTATUS(status) == 0);
}

static void fresh_tmpfs(const char *path)
{
    require_success(mount("tmpfs", path, "tmpfs", MS_NOSUID | MS_NODEV, "mode=1777,size=16m"), "mount tmpfs");
}

static void prepare_jail(char jail[PATH_MAX])
{
    char path[PATH_MAX];
    static const char *const directories[] = {
        "/mnt", "/mnt/host", "/mnt/memory", "/dev", "/dev/shm", "/tmp", "/proc",
    };

    strcpy(jail, "/run/soperator-job-mount-test-XXXXXX");
    assert(mkdtemp(jail) != NULL);
    require_success(mount(jail, jail, NULL, MS_BIND, NULL), "bind jail");
    for (size_t i = 0; i < sizeof(directories) / sizeof(directories[0]); i++) {
        join_path(path, jail, directories[i]);
        require_success(mkdir(path, 0755), "create jail directory");
    }
    for (size_t i = 0; i < sizeof(test_paths) / sizeof(test_paths[0]); i++) {
        join_path(path, jail, test_paths[i]);
        fresh_tmpfs(path);
        write_marker(path, "/service-only");
    }
    join_path(path, jail, "/proc");
    require_success(mount("proc", path, "proc", 0, NULL), "mount jail proc");
    /* A shared jail catches aliases accidentally propagated into other steps. */
    require_success(mount(NULL, jail, NULL, MS_SHARED | MS_REC, NULL), "share jail mounts");
}

static void run_session(const char *jail, bool spank, bool adopted)
{
    pid_t child = fork();

    if (child == 0) {
        struct stat original[3];
        struct jail_context context = {.pamh = NULL, .container_log_fd = STDERR_FILENO};
        const struct pam_conv conversation = {.conv = NULL, .appdata_ptr = NULL};

        require_success(pam_start("other", "root", &conversation, &context.pamh), "start PAM handle");

        for (size_t i = 0; i < sizeof(test_paths) / sizeof(test_paths[0]); i++) {
            require_success(stat(test_paths[i], &original[i]), "stat canonical mount");
        }
        if (spank) {
            require_success(change_root(jail), "SPANK jail transition");
            require_success(remount_proc(), "SPANK proc remount");
        } else {
            require_success(enter_jail(&context, jail, adopted), "PAM jail transition");
        }
        for (size_t i = 0; i < sizeof(test_paths) / sizeof(test_paths[0]); i++) {
            struct stat current;

            require_success(stat(test_paths[i], &current), "stat jailed mount");
            if (adopted) {
                assert(current.st_dev == original[i].st_dev && current.st_ino == original[i].st_ino);
                assert((current.st_mode & 07777) == 01777);
                assert_marker(test_paths[i], "/from-slurmd", true);
                assert_marker(test_paths[i], "/service-only", false);
                write_marker(test_paths[i], spank ? "/from-spank" : "/from-adopted-ssh");
            } else {
                assert_marker(test_paths[i], "/service-only", true);
                assert_marker(test_paths[i], "/from-slurmd", false);
                write_marker(test_paths[i], "/from-exempt-ssh");
            }
        }
        require_success(pam_end(context.pamh, PAM_SUCCESS), "end PAM handle");
        _exit(EXIT_SUCCESS);
    }
    wait_success(child);
}

static void run_job(const char *jail)
{
    pid_t child = fork();

    if (child == 0) {
        require_success(unshare(CLONE_NEWNS), "create job namespace");
        require_success(mount(NULL, "/", NULL, MS_SLAVE | MS_REC, NULL), "isolate job mounts");
        for (size_t i = 0; i < sizeof(test_paths) / sizeof(test_paths[0]); i++) {
            fresh_tmpfs(test_paths[i]);
            assert_marker(test_paths[i], "/from-spank", false);
            assert_marker(test_paths[i], "/from-adopted-ssh", false);
            write_marker(test_paths[i], "/from-slurmd");
        }
        run_session(jail, true, true);
        run_session(jail, false, true);
        for (size_t i = 0; i < sizeof(test_paths) / sizeof(test_paths[0]); i++) {
            char target[PATH_MAX];

            assert_marker(test_paths[i], "/from-spank", true);
            assert_marker(test_paths[i], "/from-adopted-ssh", true);
            join_path(target, jail, test_paths[i]);
            assert_marker(target, "/service-only", true);
            assert_marker(target, "/from-slurmd", false);
        }
        _exit(EXIT_SUCCESS);
    }
    wait_success(child);
}

int main(void)
{
    char jail[PATH_MAX];

    assert(is_extern_step_cgroup("0::/slurm/abc/step_extern/user/task_0\n"));
    assert(is_extern_step_cgroup("0::/step_extern\n"));
    assert(!is_extern_step_cgroup("0::/slurm/abc/step_extern_backup\n"));
    assert(!is_extern_step_cgroup("0::/slurm/abc/not_step_extern\n"));
    assert(!is_extern_step_cgroup("0::/slurm/abc/step_0\n"));
    assert(!is_extern_step_cgroup("1:memory:/slurm/abc/step_extern\n"));
    assert(!in_adopted_job_namespace());

    require_success(unshare(CLONE_NEWNS), "isolate test mounts");
    require_success(mount(NULL, "/", NULL, MS_PRIVATE | MS_REC, NULL), "make test mounts private");
    if (mkdir("/mnt/memory", 0755) != 0) {
        assert(errno == EEXIST);
    }
    prepare_jail(jail);
    for (size_t i = 0; i < sizeof(test_paths) / sizeof(test_paths[0]); i++) {
        fresh_tmpfs(test_paths[i]);
        write_marker(test_paths[i], "/outer-service-only");
    }

    run_job(jail);
    run_job(jail);
    run_session(jail, false, false);
    for (size_t i = 0; i < sizeof(test_paths) / sizeof(test_paths[0]); i++) {
        char target[PATH_MAX];

        assert_marker(test_paths[i], "/outer-service-only", true);
        assert_marker(test_paths[i], "/from-slurmd", false);
        join_path(target, jail, test_paths[i]);
        assert_marker(target, "/from-exempt-ssh", true);
    }
    puts("PASS: canonical paths, SPANK and PAM aliases, job and service isolation, exempt SSH");
    return EXIT_SUCCESS;
}
