load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "state",
    srcs = ["state.go"],
    importpath = "nebius.ai/slurm-operator/internal/controller/state",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "@io_k8s_apimachinery//pkg/runtime/schema",
        "@io_k8s_sigs_controller_runtime//pkg/client",
    ],
)
