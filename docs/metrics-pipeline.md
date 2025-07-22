# Metrics Pipeline Documentation

## Overview

The Soperator metrics pipeline provides comprehensive observability for SLURM clusters running on Kubernetes. It collects metrics from various sources, stores them in VictoriaMetrics, and provides visualization through Grafana.

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
- **Purpose**: Exports SLURM-specific metrics
- **Port**: 8080
- **Metrics**: SLURM nodes state, jobs, controller RPC diagnostics
- **Deployment**: Runs on system nodes (`slurm.nebius.ai/nodeset=system`)
- **Namespace**: `soperator` (in the SLURM cluster namespace)
- **Scrape Interval**: 30s (default)
- **Label Processing**: Automatic removal of Kubernetes metadata labels (`pod`, `instance`, `container`)
- **Documentation**: [slurm-exporter.md](slurm-exporter.md)

**Connection Example:**
```bash
kubectl port-forward -n soperator deployment/slurm-exporter 8080:8080
curl http://localhost:8080/metrics
```

**Metric Label Processing:**

The Soperator Exporter automatically applies metric relabeling to ensure counter continuity across pod restarts. The PodMonitor configuration includes default `metricRelabelConfigs` that drop volatile Kubernetes metadata labels while preserving essential business logic and operational labels.

**Automatically Dropped Labels:**
- `pod` - Pod name (e.g., `slurm-exporter-c568cb944-pwv4l`) - changes on restart
- `instance` - Pod IP/endpoint - changes on restart  
- `container` - Container name (always "exporter") - provides no value

**Preserved Labels:**
- `cluster` - Cluster identifier for multi-cluster setups
- `job` - Prometheus job name (`soperator/soperator`)
- `namespace` - Kubernetes namespace (`soperator`)
- Business logic labels: `node_name`, `state_base`, `message_type`, etc.

**Counter Continuity Benefit:**

This automatic label processing ensures that counter metrics like `slurm_node_gpu_seconds_total` maintain their cumulative values across pod restarts, providing accurate long-term statistics without manual configuration.

**Example - Before vs After Processing:**

Raw exporter output (what the exporter produces):
```
slurm_node_gpu_seconds_total{node_name="worker-0",state_base="IDLE"} 3600
```

Stored in VictoriaMetrics (after PodMonitor processing):
```
slurm_node_gpu_seconds_total{
  cluster="production",
  job="soperator/soperator", 
  namespace="soperator",
  node_name="worker-0",
  state_base="IDLE"
} 3600
```

Note: Labels like `pod="slurm-exporter-abc123"`, `instance="10.0.1.5:8080"`, and `container="exporter"` are automatically dropped to prevent metric resets during pod restarts.

#### 2. DCGM Exporter  
- **Purpose**: Exports NVIDIA GPU metrics
- **Port**: 9400
- **Metrics**: GPU temperature, power, utilization, memory, errors
- **Scrape Interval**: 15s
- **DaemonSet**: Runs on nodes with `nvidia.com/gpu.deploy.dcgm-exporter=true`

**Connection Example:**
```bash
# Find a DCGM exporter pod
kubectl get pods -n soperator -l app=nvidia-dcgm-exporter -o wide

# Port-forward to a specific pod
kubectl port-forward -n soperator <dcgm-pod-name> 9400:9400
curl http://localhost:9400/metrics
```

#### 3. Node Exporter
- **Purpose**: Exports node/system metrics
- **Port**: 9100  
- **Metrics**: CPU, memory, disk, network statistics
- **Part of**: Prometheus Operator stack

#### 4. Kubelet Metrics
- **Purpose**: Collects node and container metrics from kubelet
- **Endpoints**:
  - `/metrics` - Core kubelet metrics
  - `/metrics/cadvisor` - Container and cgroup metrics
  - `/metrics/probes` - Liveness and readiness probe metrics
  - `/metrics/resource` - Pod resource metrics
- **Scrape Method**: VMScrapes targeting node endpoints directly
- **Scrape Interval**: 30s

**Key Metrics**:
- `container_memory_usage_bytes` - Container memory usage
- `container_cpu_usage_seconds_total` - Container CPU usage
- `kubelet_pod_start_duration_seconds` - Pod startup latency
- `kubelet_running_pods` - Number of running pods per node

#### 5. Kube-state-metrics
- **Purpose**: Exports Kubernetes object metrics
- **Ports**: 
  - 8080 - Main metrics endpoint (Kubernetes object state)
  - 8081 - Telemetry endpoint (self-monitoring)
- **Metrics**: Pod state metrics (filtered subset)
- **Deployment**: Single replica deployment in `monitoring-system` namespace
- **Configuration**: `--resources=pods` with metric allowlist filtering

**Connection Example:**
```bash
# Port-forward to main metrics endpoint (8080)
kubectl port-forward -n monitoring-system svc/metrics-kube-state-metrics 8080:8080

# Get Kubernetes object metrics (tens of thousands of metrics)
curl http://localhost:8080/metrics | grep "^kube_" | wc -l

# Port-forward to telemetry endpoint (8081)  
kubectl port-forward -n monitoring-system svc/metrics-kube-state-metrics 8081:8081

# Get self-monitoring metrics (a few metrics about kube-state-metrics itself)
curl http://localhost:8081/metrics | grep "^kube_"
```

**Important Port Difference:**
- **Port 8080**: Main metrics about Kubernetes objects (pods, deployments, etc.) - this is what you want for monitoring your cluster
- **Port 8081**: Self-monitoring/telemetry metrics about the kube-state-metrics process itself (API call success rates, process health)
- The VMServiceScrape targets port 8080 to get the actual Kubernetes metrics, port 8081 is not scraped

**How it's scraped:**
```bash
# Check VMServiceScrape that targets kube-state-metrics
kubectl get vmservicescrape -n monitoring-system | grep kube-state

# Get details of the scrape configuration
kubectl get vmservicescrape -n monitoring-system metrics-victoria-metrics-k8s-stack-kube-state-metrics -o yaml

# Verify VMAgent picks it up (selectAllByDefault: true)
kubectl get vmagent -n monitoring-system metrics-victoria-metrics-k8s-stack -o jsonpath='{.spec.selectAllByDefault}'

# Check which services match the VMServiceScrape selector
kubectl get svc -A -l "app.kubernetes.io/instance=metrics,app.kubernetes.io/name=kube-state-metrics"

# Verify the service labels and ports
kubectl get svc -n monitoring-system metrics-kube-state-metrics -o yaml | grep -A20 "labels:"
kubectl get svc -n monitoring-system metrics-kube-state-metrics -o yaml | grep -A10 "ports:"
```

**Key details:**
- VMServiceScrape targets services with labels: `app.kubernetes.io/instance=metrics` AND `app.kubernetes.io/name=kube-state-metrics`
- It scrapes the `http` port (8080) for Kubernetes object metrics (tens of thousands of metrics), not the `metrics` port (8081) which only has a few self-monitoring metrics
- Applies metric relabeling to drop labels: `uid`, `container_id`, `image_id`
- Configuration: `--resources=pods` means only pod-related metrics are collected, not all Kubernetes objects

#### 6. Soperator Controller Metrics
- **Purpose**: Exports controller runtime metrics
- **Port**: 8443 (through kube-rbac-proxy)
- **Metrics**: Reconciliation metrics, controller health
- **Deployment**: Runs on system nodes with the controller manager
- **Namespace**: `soperator-system`
- **Access**: Protected by RBAC proxy, requires proper authentication

**Connection Example:**
```bash
# Direct port-forward to the controller pod (bypasses RBAC)
kubectl port-forward -n soperator-system deployment/soperator-controller-manager 8080:8080

# Access metrics directly (from the manager container, not rbac-proxy)
curl http://localhost:8080/metrics

# Note: This bypasses the RBAC proxy. In production, metrics should be 
# scraped through the rbac-proxy using proper ServiceAccount tokens.
```

**Creating a ServiceMonitor (for proper scraping):**
```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: soperator-controller-metrics
  namespace: soperator-system
spec:
  endpoints:
  - bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
    port: https
    scheme: https
    tlsConfig:
      insecureSkipVerify: true
  selector:
    matchLabels:
      control-plane: controller-manager
```

### Metrics Processing & Storage

#### VictoriaMetrics Agent (VMAgent)
- **Purpose**: Scrapes metrics from exporters and forwards to storage
- **Features**:
  - Service discovery via Kubernetes API
  - Label filtering and relabeling
  - Remote write to multiple destinations
  - Stream parsing for efficiency

**VMAgent Self-Monitoring:**
VMAgent exposes its own metrics on port 8429 for monitoring and debugging:

```bash
# Port-forward to VMAgent metrics endpoint
kubectl port-forward -n monitoring-system svc/vmagent-metrics-victoria-metrics-k8s-stack 8429:8429

# Get VMAgent operational metrics
curl http://localhost:8429/metrics

# Check scrape targets status
curl http://localhost:8429/targets

# View configuration
curl http://localhost:8429/config
```

**Adding New ServiceMonitors:**
```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: custom-app
  namespace: monitoring-system
spec:
  selector:
    matchLabels:
      app: custom-app
  endpoints:
  - port: metrics
    interval: 30s
    path: /metrics
```

Note: The slurm-exporter deployment doesn't have a Service by default. To scrape it, you can either:
1. Create a Service for the deployment and a ServiceMonitor
2. Use pod annotations for scraping (if VMAgent is configured to discover pods)

**Troubleshooting:**
```bash
# Check scrape targets
kubectl port-forward -n monitoring-system svc/vmagent-metrics-victoria-metrics-k8s-stack 8429:8429
# Visit http://localhost:8429/targets

# Check VMAgent logs
kubectl logs -n monitoring-system deployment/vmagent-metrics-victoria-metrics-k8s-stack -c vmagent

# Check VMAgent configuration
kubectl get vmagent -n monitoring-system -o yaml

# Check specific VMAgent resource configuration (the only one for the time being)
kubectl get vmagent -n monitoring-system metrics-victoria-metrics-k8s-stack -o yaml

# List all ServiceMonitors
kubectl get servicemonitor -A

# Debug remote write
kubectl get vmagent -n monitoring-system metrics-victoria-metrics-k8s-stack -o yaml | grep -A10 remoteWrite
```

#### VictoriaMetrics Single (VMSingle)
- **Purpose**: Time-series database for metrics storage
- **Port**: 8429
- **Retention**: 30 days
- **Storage**: 30Gi persistent volume
- **API**: Prometheus-compatible query API

**Connection Example:**
```bash
# Port-forward to VMSingle
kubectl port-forward -n monitoring-system svc/vmsingle-metrics-victoria-metrics-k8s-stack 8429:8429

# Query metrics with curl
curl "http://localhost:8429/api/v1/query?query=up"

# Query with time range
curl "http://localhost:8429/api/v1/query_range?query=slurm_nodes_total&start=2024-01-01T00:00:00Z&end=2024-01-01T01:00:00Z&step=60s"

# Get label values
curl "http://localhost:8429/api/v1/label/job/values"
```

**Common Queries:**
```promql
# SLURM node states
slurm_nodes_total{state="idle"}

# GPU utilization
DCGM_FI_DEV_GPU_UTIL{job="slurm-worker"}

# Memory usage by node
100 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes * 100)

# Failed SLURM jobs
rate(slurm_job_failed_total[5m])
```

#### Remote Write to Nebius Cloud
- **Endpoint**: `https://write.monitoring.{region}.nebius.cloud/projects/{projectId}/buckets/soperator/prometheus`
- **Authentication**: Bearer token from `/mnt/cloud-metadata/tsa-token`
- **When**: Enabled with `publicEndpointEnabled: true`

### Logs

For information about the logs pipeline (VictoriaLogs, OpenTelemetry collectors, log search), see [logs-pipeline.md](logs-pipeline.md).

### Visualization

#### Grafana
- **Purpose**: Metrics visualization and dashboards
- **Port**: 80
- **Authentication**: Anonymous access enabled (Editor role)
- **Features**:
  - Pre-configured dashboards
  - VictoriaMetrics datasource
  - Dashboard discovery from ConfigMaps
  - Loki/VictoriaLogs integration

**Connection Example:**
```bash
# Port-forward to Grafana
kubectl port-forward -n monitoring-system svc/metrics-grafana 3000:80

# Access in browser: http://localhost:3000

# List all available dashboards
kubectl get configmap -n monitoring-system -l grafana_dashboard=1 -o custom-columns=NAME:.metadata.name,CHART:.metadata.labels."helm\.sh/chart"
```

**Available Dashboards:**
The cluster includes 16 pre-configured dashboards from two sources:

**Victoria Metrics K8s Stack Dashboards** (12 dashboards from `victoria-metrics-k8s-stack` helm chart):
- Grafana Overview - Grafana system metrics
- Kubelet - Node kubelet metrics
- Kubernetes System API Server - API server performance
- Kubernetes Views (Global, Namespaces, Nodes, Pods) - K8s resource views
- Node Exporter Full - Comprehensive node metrics
- VictoriaMetrics (Operator, Single Node, VMAgent, VMAlert) - Monitoring stack health

*Note: These dashboards are defined in the third-party `victoria-metrics-k8s-stack` helm chart under `files/dashboards/generated/` and are automatically deployed based on which components are enabled in the cluster.*

**Soperator Dashboards** (4 custom dashboards):
- Soperator Cluster Health - Overall SLURM cluster health
- Soperator Jobs Overview - SLURM job metrics and status
- Soperator Workers Detailed Stats - Detailed worker node statistics
- Soperator Workers Overview - Worker node summary

**Dashboard Discovery:**
- Dashboards are deployed as ConfigMaps with label `grafana_dashboard: "1"`
- Grafana watches these namespaces: `soperator`, `soperator-system`, `gpu-operator`, `monitoring-system`, `logs-system`
- New dashboards are automatically discovered and loaded

**Custom Dashboard Import:**
1. Create ConfigMap with dashboard JSON:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: custom-dashboard
  namespace: monitoring-system
  labels:
    grafana_dashboard: "1"
data:
  dashboard.json: |
    {
      "dashboard": { ... }
    }
```

2. Apply the ConfigMap:
```bash
kubectl apply -f custom-dashboard.yaml
```


## Service Endpoints Reference

### Internal Cluster Endpoints

| Service | Namespace | Endpoint | Port |
|---------|-----------|----------|------|
| VMSingle | monitoring-system | vmsingle-metrics-victoria-metrics-k8s-stack | 8429 |
| VMAgent | monitoring-system | vmagent-metrics-victoria-metrics-k8s-stack | 8429 |
| Grafana | monitoring-system | metrics-victoria-metrics-k8s-stack-grafana | 80 |
| SLURM Exporter | soperator | slurm-exporter (deployment) | 8080 |
| Soperator AcctDB Metrics | soperator | soperator-acct-db-metrics | 9104 |
| DCGM Exporter | soperator | nvidia-dcgm-exporter | 9400 |

### Authentication

- **Grafana**: Anonymous access enabled with Editor role
- **VictoriaMetrics**: No authentication for internal access
- **Nebius Cloud**: Bearer token authentication (automatic)

### Useful Commands

```bash
# List all ServiceMonitors
kubectl get servicemonitor -A

# View VMAgent configuration
kubectl get vmagent -n monitoring-system -o yaml

# Check scrape targets
kubectl port-forward -n monitoring-system svc/vmagent-metrics-victoria-metrics-k8s-stack 8429:8429
curl http://localhost:8429/targets

# View Grafana datasources
kubectl get cm -n monitoring-system metrics-victoria-metrics-k8s-stack-grafana -o yaml
```

