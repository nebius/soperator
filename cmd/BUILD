load("@rules_go//go:def.bzl", "go_binary", "go_cross_binary", "go_library")

go_library(
    name = "cmd_lib",
    srcs = ["main.go"],
    importpath = "nebius.ai/slurm-operator/cmd",
    visibility = ["//visibility:private"],
    deps = [
        "//msp/slurm-service/internal/operator/api/v1:api",
        "//msp/slurm-service/internal/operator/internal/controller",
        "@io_k8s_apimachinery//pkg/runtime",
        "@io_k8s_apimachinery//pkg/util/runtime",
        "@io_k8s_client_go//kubernetes/scheme",
        "@io_k8s_client_go//plugin/pkg/client/auth",
        "@io_k8s_sigs_controller_runtime//:controller-runtime",
        "@io_k8s_sigs_controller_runtime//pkg/healthz",
        "@io_k8s_sigs_controller_runtime//pkg/log/zap",
        "@io_k8s_sigs_controller_runtime//pkg/metrics/server",
        "@io_k8s_sigs_controller_runtime//pkg/webhook",
    ],
)

go_binary(
    name = "cmd",
    embed = [":cmd_lib"],
    visibility = ["//msp/slurm-service:__subpackages__"],
)

go_cross_binary(
    name = "manager",
    platform = "//nebazel/platforms:linux_amd64",
    target = ":cmd",
    visibility = ["//msp/slurm-service:__subpackages__"],
)
