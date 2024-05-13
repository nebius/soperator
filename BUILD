# gazelle:prefix nebius.ai/slurm-operator

load("@aspect_bazel_lib//lib:copy_to_directory.bzl", "copy_to_directory")

# tools (aka KUBEBUILDER_ASSETS) are a collection of binaries that are used by
# kubebuilder for envtests.
copy_to_directory(
    name = "envtest_tools",
    srcs = [
        ":etcd",
        ":kube-apiserver",
        ":kubectl",
    ],
    include_external_repositories = ["*"],
    visibility = ["//visibility:public"],
)

alias(
    name = "etcd",
    actual = select({
        "@rules_go//go/platform:darwin_amd64": "@kubebuilder_tools_darwin_amd64//:etcd",
        "@rules_go//go/platform:darwin_arm64": "@kubebuilder_tools_darwin_arm64//:etcd",
        "@rules_go//go/platform:linux_amd64": "@kubebuilder_tools_linux_amd64//:etcd",
        "@rules_go//go/platform:linux_arm64": "@kubebuilder_tools_linux_arm64//:etcd",
    }),
    visibility = ["//visibility:public"],
)

alias(
    name = "kube-apiserver",
    actual = select({
        "@rules_go//go/platform:darwin_amd64": "@kubebuilder_tools_darwin_amd64//:kube-apiserver",
        "@rules_go//go/platform:darwin_arm64": "@kubebuilder_tools_darwin_arm64//:kube-apiserver",
        "@rules_go//go/platform:linux_amd64": "@kubebuilder_tools_linux_amd64//:kube-apiserver",
        "@rules_go//go/platform:linux_arm64": "@kubebuilder_tools_linux_arm64//:kube-apiserver",
    }),
    visibility = ["//visibility:public"],
)

alias(
    name = "kubectl",
    actual = select({
        "@rules_go//go/platform:darwin_amd64": "@kubebuilder_tools_darwin_amd64//:kubectl",
        "@rules_go//go/platform:darwin_arm64": "@kubebuilder_tools_darwin_arm64//:kubectl",
        "@rules_go//go/platform:linux_amd64": "@kubebuilder_tools_linux_amd64//:kubectl",
        "@rules_go//go/platform:linux_arm64": "@kubebuilder_tools_linux_arm64//:kubectl",
    }),
    visibility = ["//visibility:public"],
)

filegroup(
    name = "config",
    srcs = glob(["**/*.yaml"]),
    visibility = [
        "//msp/slurm-service/internal/operator:__subpackages__",
    ],
)

filegroup(
    name = "crds",
    srcs = glob(["config/crd/bases/*.yaml"]),
    visibility = [
        "//msp:__subpackages__",
    ],
)
