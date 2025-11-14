# NFS Server in Kubernetes

NFS server is deployed using the custom Helm chart to back the `/home` directory inside `jail` for Login and Worker nodes.

* `csi-driver-nfs` is used for provisioning volumes based on the NFS server
* A `node-exporter` is included as a sidecar to monitor the NFS server performance
