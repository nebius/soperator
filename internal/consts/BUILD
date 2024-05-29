load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "consts",
    srcs = [
        "annotation.go",
        "component.go",
        "configmap.go",
        "container.go",
        "image.go",
        "jail.go",
        "label.go",
        "pvc.go",
        "secret.go",
        "service.go",
        "slurm.go",
        "volume.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/consts",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "@io_k8s_api//core/v1:core",
        "@io_k8s_apimachinery//pkg/api/resource",
    ],
)
