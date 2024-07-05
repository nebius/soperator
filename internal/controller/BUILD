load("@rules_go//go:def.bzl", "go_test")

go_test(
    name = "controller_test",
    srcs = [
        "common_subjects_test.go",
        "fixtures_test.go",
        "helpers_test.go",
        "suite_test.go",
    ],
    data = [
        "//msp/slurm-service/internal/operator:config",
        "//msp/slurm-service/internal/operator:envtest_tools",
    ],
    env = {
        "KUBEBUILDER_ASSETS": "../../envtest_tools",
    },
    deps = [
        "//msp/slurm-service/internal/operator/api/v1:api",
        "@com_github_onsi_ginkgo_v2//:ginkgo",
        "@com_github_onsi_gomega//:gomega",
        "@io_k8s_apimachinery//pkg/apis/meta/v1:meta",
        "@io_k8s_apimachinery//pkg/types",
        "@io_k8s_client_go//kubernetes/scheme",
        "@io_k8s_client_go//rest",
        "@io_k8s_sigs_controller_runtime//:controller-runtime",
        "@io_k8s_sigs_controller_runtime//pkg/client",
        "@io_k8s_sigs_controller_runtime//pkg/envtest",
        "@io_k8s_sigs_controller_runtime//pkg/log",
        "@io_k8s_sigs_controller_runtime//pkg/log/zap",
    ],
)
