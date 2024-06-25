{{/* Populate jail Job image */}}
{{- define "slurm-cluster.image.populateJail" -}}
    {{- (default "cr.nemax.nebius.cloud/crnonjecps8pifr7am4i/populate_jail:latest" .Values.populateJail.image) | quote -}}
{{- end }}

{{/* NCCL benchmark CronJob image */}}
{{- define "slurm-cluster.image.ncclBenchmark" -}}
    {{- (default "cr.nemax.nebius.cloud/crnonjecps8pifr7am4i/nccl_benchmark:latest" .Values.populateJail.image) | quote -}}
{{- end }}

{{/* Slurmctld image */}}
{{- define "slurm-cluster.image.slurmctld" -}}
    {{- (default "cr.nemax.nebius.cloud/crnonjecps8pifr7am4i/controller_slurmctld:latest" .Values.slurmNodeImages.slurmctld) | quote -}}
{{- end }}

{{/* Slurmd image */}}
{{- define "slurm-cluster.image.slurmd" -}}
    {{- (default "cr.nemax.nebius.cloud/crnonjecps8pifr7am4i/worker_slurmd:latest" .Values.slurmNodeImages.slurmd) | quote -}}
{{- end }}

{{/* Sshd image */}}
{{- define "slurm-cluster.image.sshd" -}}
    {{- (default "cr.nemax.nebius.cloud/crnonjecps8pifr7am4i/login_sshd:latest" .Values.slurmNodeImages.sshd) | quote -}}
{{- end }}

{{/* Munge image */}}
{{- define "slurm-cluster.image.munge" -}}
    {{- (default "cr.nemax.nebius.cloud/crnonjecps8pifr7am4i/munge:latest" .Values.slurmNodeImages.munge) | quote -}}
{{- end }}
