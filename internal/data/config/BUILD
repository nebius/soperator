load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "config",
    srcs = [
        "cgroup_conf.go",
        "plugstack_conf.go",
        "slurm_conf.go",
        "types.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/data/config",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "//msp/slurm-service/internal/operator/internal/consts",
        "//msp/slurm-service/internal/operator/internal/models/k8s",
        "//msp/slurm-service/internal/operator/internal/models/k8s/naming",
    ],
)
