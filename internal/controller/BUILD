load("@rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "controller",
    srcs = ["slurmcluster_controller.go"],
    importpath = "nebius.ai/slurm-operator/internal/controller",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "//msp/slurm-service/internal/operator/api/v1:api",
        "@io_k8s_apimachinery//pkg/runtime",
        "@io_k8s_sigs_controller_runtime//:controller-runtime",
        "@io_k8s_sigs_controller_runtime//pkg/client",
        "@io_k8s_sigs_controller_runtime//pkg/log",
    ],
)

go_test(
    name = "controller_test",
    srcs = [
        "slurmcluster_controller_test.go",
        "suite_test.go",
    ],
    embed = [":controller"],
    deps = [
        "//msp/slurm-service/internal/operator/api/v1:api",
        "@com_github_onsi_ginkgo_v2//:ginkgo",
        "@com_github_onsi_gomega//:gomega",
        "@io_k8s_apimachinery//pkg/api/errors",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:meta",
        "@io_k8s_apimachinery//pkg/types",
        "@io_k8s_client_go//kubernetes/scheme",
        "@io_k8s_client_go//rest",
        "@io_k8s_sigs_controller_runtime//pkg/client",
        "@io_k8s_sigs_controller_runtime//pkg/envtest",
        "@io_k8s_sigs_controller_runtime//pkg/log",
        "@io_k8s_sigs_controller_runtime//pkg/log/zap",
        "@io_k8s_sigs_controller_runtime//pkg/reconcile",
    ],
)
