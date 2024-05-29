load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "consts",
    srcs = [
        "annotation.go",
        "component.go",
        "configmap.go",
        "indexfield.go",
        "label.go",
        "secret.go",
        "service.go",
        "slurm.go",
        "version.go",
        "volume.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/consts",
    visibility = ["//msp/slurm-service:__subpackages__"],
)
