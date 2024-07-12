load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "reconciler",
    srcs = [
        "k8s_configmap.go",
        "k8s_cronjob.go",
        "k8s_job.go",
        "k8s_secret.go",
        "k8s_service.go",
        "k8s_statefulset.go",
        "reconciler.go",
        "versioning.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/controller/reconciler",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "//msp/slurm-service/internal/operator/api/v1:api",
        "//msp/slurm-service/internal/operator/internal/consts",
        "//msp/slurm-service/internal/operator/internal/logfield",
        "@com_github_pkg_errors//:errors",
        "@io_k8s_api//apps/v1:apps",
        "@io_k8s_api//batch/v1:batch",
        "@io_k8s_api//core/v1:core",
        "@io_k8s_apimachinery//pkg/api/errors",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:meta",
        "@io_k8s_apimachinery//pkg/runtime",
        "@io_k8s_client_go//tools/record",
        "@io_k8s_sigs_controller_runtime//:controller-runtime",
        "@io_k8s_sigs_controller_runtime//pkg/client",
        "@io_k8s_sigs_controller_runtime//pkg/log",
        "@io_k8s_sigs_yaml//:yaml",
    ],
)
