load("@rules_go//go:def.bzl", "go_library")

go_library(
    name = "clustercontroller",
    srcs = [
        "node_controller.go",
        "node_worker.go",
        "reconcile.go",
    ],
    importpath = "nebius.ai/slurm-operator/internal/controller/clustercontroller",
    visibility = ["//msp/slurm-service:__subpackages__"],
    deps = [
        "//msp/slurm-service/internal/operator/api/v1:api",
        "//msp/slurm-service/internal/operator/internal/models/slurm:smodels",
        "//msp/slurm-service/internal/operator/internal/node/controller",
        "//msp/slurm-service/internal/operator/internal/reconciler",
        "@io_k8s_api//apps/v1:apps",
        "@io_k8s_api//core/v1:core",
        "@io_k8s_apimachinery//pkg/api/errors",
        "@io_k8s_apimachinery//pkg/api/meta",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:meta",
        "@io_k8s_apimachinery//pkg/types",
        "@io_k8s_sigs_controller_runtime//:controller-runtime",
        "@io_k8s_sigs_controller_runtime//pkg/log",
    ],
)
