Feature: Filesystem layout
  @soperator_version_>=4.0.0
  Scenario: Login and GPU worker expose the configured filesystem layout
    Given a GPU worker is available for filesystem checks
    When the filesystem layout is inspected on one login node and one GPU worker
    # Image storage is required on workers and checked on login only when configured.
    Then the following mounts have their expected filesystem types and permissions:
      | Mount             | Path               | Filesystem | Mode |
      | Jail root         | /                  | virtiofs   | 755  |
      | NFS home          | configured         | nfs4       | 2755 |
      | Shared data       | configured         | virtiofs   | 777  |
      | Image storage     | configured         | ext4       | 755  |
      | Scratch           | configured         | ext4       | 777  |
      | Temporary storage | /tmp               | configured | 777  |
      | Memory            | /mnt/memory        | tmpfs      | 1777 |
      | NVIDIA driver     | /run/nvidia/driver | configured | 755  |
    And each mount has the capacity expected from its configured backing storage
    And small files can be created, read and deleted on every writable data mount
    And the login node and worker exchange files in both directions through NFS home and shared data
