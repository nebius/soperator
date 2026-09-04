Feature: Soperator utilities

  @soperator_version_>=4.0.0
  Scenario: Filesystem usage filters user-relevant mounts
    Given a worker is selected for Soperator utility checks
    When fs_usage runs without a filter and with every supported filter
    Then every fs_usage result contains only its expected filesystem types

  @gpu @soperator_version_>=5.0.0
  Scenario: Instance login executes commands on the worker host
    Given a GPU worker is selected for Soperator utility checks
    When soperator_instance_login queries the worker host by name and instance ID
    Then both queries find the host kubelet process

  @gpu @soperator_version_>=4.0.0
  Scenario: Task information describes a GPU allocation
    Given a GPU worker is selected for Soperator utility checks
    When slurm_task_info runs as the prolog of a one-GPU task
    Then task info reports the selected node, rank, CPU, GPU, and CUDA device

  @gpu @soperator_version_>=4.0.0
  Scenario: NVIDIA bug report is collected from a worker
    Given a GPU worker is selected for Soperator utility checks
    When worker_nvidia_bug_report downloads a report by instance ID
    Then the report is a non-empty valid gzip file
