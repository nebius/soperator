load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "values",
    srcs = [
        "slurm_cluster.go",
        "slurm_controller.go",
        "slurm_jail.go",
        "slurm_login.go",
        "slurm_periodic_checks.go",
        "slurm_worker.go",
        "types.go",
        "validate.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/values",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "//msp/slurm-service/internal/operator/api/v1:api",
        "//msp/slurm-service/internal/operator/internal/consts",
        "//msp/slurm-service/internal/operator/internal/naming",
        "//msp/slurm-service/internal/operator/internal/utils",
        "@io_k8s_api//core/v1:core",
        "@io_k8s_apimachinery//pkg/types",
        "@io_k8s_apimachinery//pkg/util/intstr",
        "@io_k8s_sigs_controller_runtime//pkg/log",
        "@io_k8s_utils//ptr",
    ],
)
