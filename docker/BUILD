load("@rules_oci//oci:defs.bzl", "oci_push")
load("//nebazel/bzl:deploy.bzl", "nebius_image")

nebius_image(
    name = "image",
    entrypoint = ["/manager"],
    repo_tags = ["slurm-operator-controller:latest"],
    target = "//msp/slurm-service/internal/operator/cmd:manager",
)

oci_push(
    name = "push_poc",
    image = ":image",
    repository = "cr.nemax.nebius.cloud/crnsp9v7ps4c2uks5n6a/slurm-operator-controller",
)

oci_push(
    name = "push_prod",
    image = ":image",
    repository = "cr.nemax.nebius.cloud/crncqadmoafmltf8el6q/slurm-operator-controller",
)
