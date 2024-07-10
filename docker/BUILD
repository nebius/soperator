load("@rules_oci//oci:defs.bzl", "oci_push")
load("//nebazel/bzl:deploy.bzl", "nebius_image")

nebius_image(
    name = "image",
    entrypoint = ["/manager"],
    repo_tags = ["slurm-operator:latest"],
    target = "//msp/slurm-service/internal/operator/cmd:manager",
)

oci_push(
    name = "push_unstable",
    image = ":image",
    repository = "cr.nemax.nebius.cloud/crnbu823dealq64cp1s6/slurm-operator",
)

oci_push(
    name = "push_stable",
    image = ":image",
    repository = "cr.nemax.nebius.cloud/crnlu9nio77sg3p8n5bi/slurm-operator",
)
