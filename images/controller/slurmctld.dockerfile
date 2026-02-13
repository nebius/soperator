# syntax=docker.io/docker/dockerfile-upstream:1.20.0

ARG SLURM_VERSION

FROM golang:1.25 AS go-base

WORKDIR /build

# Layer 1: Go modules (changes rarely)
COPY go.mod go.sum ./
RUN go mod download

# Layer 2: Shared code (changes moderately)
COPY api api
COPY internal internal
COPY pkg pkg

# Build power-manager binary
FROM go-base AS powermanager_builder

ARG GO_LDFLAGS=""
ARG CGO_ENABLED=0
ARG GOOS=linux

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/powermanager cmd/powermanager
RUN --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$GOOS CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -v -o power-manager ./cmd/powermanager

# https://github.com/nebius/ml-containers/pull/55
FROM cr.eu-north1.nebius.cloud/ml-containers/slurm:${SLURM_VERSION}-20260205130055 AS controller_slurmctld

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

# Expose the port used for accessing slurmctld
EXPOSE 6817

# Create dir and file for multilog hack
RUN mkdir -p /var/log/slurm/multilog && \
    touch /var/log/slurm/multilog/current && \
    ln -s /var/log/slurm/multilog/current /var/log/slurm/slurmctld.log

# Copy power-manager binary for ephemeral node power management
COPY --from=powermanager_builder /build/power-manager /opt/soperator/bin/power-manager
RUN chmod 755 /opt/soperator/bin/power-manager

# Copy power management scripts for Slurm ResumeProgram/SuspendProgram
COPY images/controller/power_resume.sh /opt/soperator/bin/power_resume.sh
COPY images/controller/power_suspend.sh /opt/soperator/bin/power_suspend.sh
RUN chmod 755 /opt/soperator/bin/power_resume.sh /opt/soperator/bin/power_suspend.sh

# Copy & run the entrypoint script
COPY images/controller/slurmctld_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmctld_entrypoint.sh
ENTRYPOINT ["/opt/bin/slurm/slurmctld_entrypoint.sh"]

