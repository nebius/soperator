# Jail Logs Sync Image

This image provides a lightweight Alpine-based container with rsync pre-installed for syncing logs from local worker filesystems to the shared jail filesystem.

## Building

To build this image, use the project's standard Makefile command from the root directory:

```bash
make docker-build IMAGE_NAME=jail-logs-sync DOCKERFILE=jail-logs-sync/jail-logs-sync.dockerfile
```

This will build the image and tag it according to the project's versioning scheme.

## Pushing

To push the image to the registry:

```bash
make docker-push IMAGE_NAME=jail-logs-sync
```

## Usage

This image is used by the `jail-logs-sync` DaemonSet in the `soperator-fluxcd` Helm chart. It runs a sync script that periodically copies logs from the local spool directory to the shared jail filesystem.