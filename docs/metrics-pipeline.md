# Metrics Pipeline Documentation

## Overview

The Soperator metrics pipeline provides observability for SLURM clusters running on Kubernetes.
It collects metrics from various sources, stores them in VictoriaMetrics, and provides visualization through Grafana.

## Architecture

```
┌────────────────────────────────────────────────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│                      System Nodes                          │     │   GPU Nodes     │     │ All K8s Nodes   │
│ ┌─────────────┐ ┌──────────────┐ ┌──────────────────────┐  │     │ ┌─────────────┐ │     │ ┌─────────────┐ │
│ │   SLURM     │ │  Soperator   │ │   Kube-state         │  │     │ │    DCGM     │ │     │ │    Node     │ │
│ │  Exporter   │ │  Controller  │ │   metrics            │  │     │ │  Exporter   │ │     │ │  Exporter   │ │
│ │   :8080     │ │    :8443     │ │     :8081            │  │     │ │   :9400     │ │     │ │   :9100     │ │
│ └─────────────┘ └──────────────┘ └──────────────────────┘  │     │ └─────────────┘ │     │ └─────────────┘ │
└────────────────────────────┬───────────────────────────────┘     └────────┬────────┘     └────────┬────────┘
                             │                                              │                       │
                             └──────────────────────────────────────────────┴───────────────────────┘
                                                             │
                                                ┌────────────▼────────────┐
                                                │   VictoriaMetrics       │
                                                │   Agent (VMAgent)       │
                                                │   Scrapes & Forwards    │
                                                └────────────┬────────────┘
                                                             │
                                            ┌────────────────┴───────────────┐
                                            │                                │
                                 ┌──────────▼──────────┐          ┌──────────▼──────────┐
                                 │ VictoriaMetrics     │          │ Nebius Cloud        │
                                 │ Single (VMSingle)   │          │ Monitoring          │
                                 │ Local Storage       │          │ (Remote Write)      │
                                 └──────────┬──────────┘          └─────────────────────┘
                                            │
                                 ┌──────────▼──────────┐
                                 │      Grafana        │
                                 │   Visualization     │
                                 └─────────────────────┘
```

## Components

### Metrics Collection

#### 1. Soperator Exporter (slurm-exporter)
- Purpose: Exports SLURM-specific metrics
- Port: 8080
- Metrics: SLURM nodes state, jobs, controller RPC diagnostics
- Deployment: Runs on system nodes (`slurm.nebius.ai/nodeset=system`)
- Namespace: `soperator` (in the SLURM cluster namespace)
- Scrape Interval: 30s (default)
- Label Processing: Automatic removal of Kubernetes metadata labels (`pod`, `instance`, `container`)
- Documentation: [slurm-exporter.md](slurm-exporter.md)

Connection Example:
```bash
kubectl port-forward -n soperator deployment/slurm-exporter 8080:8080
curl http://localhost:8080/metrics
```

The exporter applies metric relabeling to drop volatile Kubernetes labels (`pod`, `instance`, `container`) for counter continuity across restarts.

#### 2. DCGM Exporter
- Purpose: Exports NVIDIA GPU metrics
- Port: 9400
- Metrics: GPU temperature, power, utilization, memory, errors
- Scrape Interval: 15s
- DaemonSet: Runs on nodes with `nvidia.com/gpu.deploy.dcgm-exporter=true`

Connection Example:
```bash
# Port-forward to a DCGM exporter pod
kubectl port-forward -n soperator deployment/nvidia-dcgm-exporter 9400:9400
curl http://localhost:9400/metrics
```

#### 3. Node Exporter
- Purpose: Exports node/system metrics
- Port: 9100
- Metrics: CPU, memory, disk, network statistics
- Part of: Prometheus Operator stack

#### 4. Kubelet Metrics
- Purpose: Collects node and container metrics from kubelet
- Endpoints:
  - `/metrics` - Core kubelet metrics
  - `/metrics/cadvisor` - Container and cgroup metrics
  - `/metrics/probes` - Liveness and readiness probe metrics
  - `/metrics/resource` - Pod resource metrics
- Scrape Method: VMScrapes targeting node endpoints directly
- Scrape Interval: 30s

Key Metrics:
- `container_memory_usage_bytes` - Container memory usage
- `container_cpu_usage_seconds_total` - Container CPU usage
- `kubelet_pod_start_duration_seconds` - Pod startup latency
- `kubelet_running_pods` - Number of running pods per node

#### 5. Kube-state-metrics
- Purpose: Exports Kubernetes object metrics
- Ports:
  - 8080 - Main metrics endpoint (Kubernetes object state)
  - 8081 - Telemetry endpoint (self-monitoring)
- Metrics: Pod state metrics (filtered subset)
- Deployment: Single replica deployment in `monitoring-system` namespace
- Configuration: `--resources=pods` with metric allowlist filtering

Connection Example:
```bash
# Port-forward to main metrics endpoint
kubectl port-forward -n monitoring-system svc/metrics-kube-state-metrics 8080:8080
curl http://localhost:8080/metrics
```

Note: Port 8080 provides Kubernetes object metrics, while port 8081 provides self-monitoring metrics. VMServiceScrape targets port 8080 for cluster monitoring.

#### 6. Soperator Controller Metrics
- Purpose: Exports controller runtime metrics
- Port: 8443 (through kube-rbac-proxy)
- Metrics: Reconciliation metrics, controller health
- Deployment: Runs on system nodes with the controller manager
- Namespace: `soperator-system`
- Access: Protected by RBAC proxy, requires proper authentication

Connection Example:
```bash
# Port-forward to controller (bypasses RBAC)
kubectl port-forward -n soperator-system deployment/soperator-controller-manager 8080:8080
curl http://localhost:8080/metrics
```

Note: Production scraping requires a ServiceMonitor with proper RBAC authentication.

### Metrics Processing & Storage

#### VictoriaMetrics Agent (VMAgent)
- Purpose: Scrapes metrics from exporters and forwards to storage
- Features:
  - Service discovery via Kubernetes API
  - Label filtering and relabeling
  - Remote write to multiple destinations
  - Stream parsing for efficiency

VMAgent exposes operational metrics on port 8429 for monitoring and debugging.

#### VictoriaMetrics Single (VMSingle)
- Purpose: Time-series database for metrics storage
- Port: 8429
- Retention: 30 days
- Storage: 30Gi persistent volume
- API: Prometheus-compatible query API

Connection Example:
```bash
# Port-forward to VMSingle
kubectl port-forward -n monitoring-system svc/vmsingle-metrics-victoria-metrics-k8s-stack 8429:8429

# Query metrics
curl "http://localhost:8429/api/v1/query?query=up"
```

#### Remote Write to Nebius Cloud
- Endpoint: `https://write.monitoring.{region}.nebius.cloud/projects/{projectId}/buckets/soperator/prometheus`
- Authentication: Bearer token from `/mnt/cloud-metadata/tsa-token`
- When: Enabled with `publicEndpointEnabled: true`

### Visualization

#### Grafana
- Purpose: Metrics visualization and dashboards
- Port: 80
- Authentication: Anonymous access enabled (Editor role)
- Features:
  - Pre-configured dashboards
  - VictoriaMetrics datasource
  - Dashboard discovery from ConfigMaps
  - Loki/VictoriaLogs integration

Connection Example:
```bash
# Port-forward to Grafana
kubectl port-forward -n monitoring-system svc/metrics-grafana 3000:80
# Access in browser: http://localhost:3000
```

Pre-configured Dashboards:
- Victoria Metrics K8s Stack: Grafana, Kubelet, Kubernetes system, Node Exporter, VictoriaMetrics health
- Soperator Custom: Cluster health, Jobs overview, Workers stats and overview

Dashboards are auto-discovered from ConfigMaps with label `grafana_dashboard: "1"` in monitored namespaces.
