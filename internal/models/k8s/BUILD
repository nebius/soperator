load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "k8s",
    srcs = [
        "image.go",
        "label.go",
        "pvc.go",
        "resourceful.go",
        "service.go",
        "statefulset.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/models/k8s",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "//msp/slurm-service/internal/operator/api/v1:api",
        "//msp/slurm-service/internal/operator/internal/consts",
        "//msp/slurm-service/internal/operator/internal/models/k8s/naming",
        "@io_k8s_api//core/v1:core",
        "@io_k8s_apimachinery//pkg/api/resource",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:meta",
        "@io_k8s_apimachinery//pkg/types",
        "@io_k8s_apimachinery//pkg/util/intstr",
    ],
)
