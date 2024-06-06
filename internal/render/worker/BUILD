load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "worker",
    srcs = [
        "container.go",
        "service.go",
        "statefulset.go",
        "volume.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/render/worker",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "//msp/slurm-service/internal/operator/api/v1:api",
        "//msp/slurm-service/internal/operator/internal/consts",
        "//msp/slurm-service/internal/operator/internal/render/common",
        "//msp/slurm-service/internal/operator/internal/utils",
        "//msp/slurm-service/internal/operator/internal/values",
        "@io_k8s_api//apps/v1:apps",
        "@io_k8s_api//core/v1:core",
        "@io_k8s_apimachinery//pkg/api/resource",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:meta",
        "@io_k8s_apimachinery//pkg/util/intstr",
    ],
)
