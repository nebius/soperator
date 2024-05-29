load("@aspect_bazel_lib//lib:copy_to_directory.bzl", "copy_to_directory")
load("@rules_go//go:def.bzl", "go_library")

copy_to_directory(
    name = "version_txt",
    srcs = ["//msp/slurm-service/internal/operator/config:chart_version"],
    out = "v",
    replace_prefixes = {
        "msp/slurm-service/internal/operator/config/chart.version": "version.txt",
    },
    visibility = ["//msp/slurm-service:__subpackages__"],
)

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
    embedsrcs = [":version_txt"],  # keep
    importpath = "nebius.ai/slurm-operator/internal/consts",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "@io_k8s_api//core/v1:core",
        "@io_k8s_apimachinery//pkg/api/resource",
    ],
)
