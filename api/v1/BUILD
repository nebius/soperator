load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "api",
    srcs = [
        "groupversion_info.go",
        "slurmcluster_types.go",
        "zz_generated.deepcopy.go",
    ],
    importpath = "nebius.ai/slurm-operator/api/v1",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "@io_k8s_apimachinery//pkg/apis/meta/v1:meta",
        "@io_k8s_apimachinery//pkg/runtime",
        "@io_k8s_apimachinery//pkg/runtime/schema",
        "@io_k8s_sigs_controller_runtime//pkg/scheme",
    ],
)
