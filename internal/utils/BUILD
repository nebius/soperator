load("@rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "utils",
    srcs = [
        "get_by.go",
        "oneof.go",
        "unique.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/utils",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = ["@org_golang_x_exp//constraints"],
)

go_test(
    name = "utils_test",
    size = "small",
    srcs = [
        "get_by_test.go",
        "oneof_test.go",
        "unique_test.go",
    ],
    deps = [
        ":utils",
        "@com_github_stretchr_testify//assert",
    ],
)
