load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "consts",
    srcs = [
        "annotation.go",
        "component.go",
        "configmap.go",
        "container.go",
        "cronjob.go",
        "indexfield.go",
        "job.go",
        "label.go",
        "nccl_topology_type.go",
        "secret.go",
        "service.go",
        "slurm.go",
        "statefulset.go",
        "version.go",
        "volume.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/consts",
    visibility = ["//msp/slurm-service:__subpackages__"],
)
