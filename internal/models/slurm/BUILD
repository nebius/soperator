load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "smodels",
    srcs = [
        "cluster.go",
        "controller.go",
        "worker.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/models/slurm",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "//msp/slurm-service/internal/operator/api/v1:api",
        "//msp/slurm-service/internal/operator/internal/consts",
        "//msp/slurm-service/internal/operator/internal/models/k8s",
        "@io_k8s_apimachinery//pkg/types",
    ],
)
