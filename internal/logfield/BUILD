load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "logfield",
    srcs = [
        "fields.go",
        "resource.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/logfield",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "//msp/slurm-service/internal/operator/internal/consts",
        "@io_k8s_sigs_controller_runtime//pkg/client",
    ],
)
