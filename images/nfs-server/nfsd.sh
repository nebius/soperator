#!/bin/bash

# NFS Server startup script
# Supports graceful shutdown, quick recovery, and tuned parameters

set -euo pipefail

# Default values for NFS server configuration
SHARED_DIRECTORY="${SHARED_DIRECTORY:-}"
PERMITTED="${PERMITTED:-*}"
READ_ONLY="${READ_ONLY:-}"
SYNC="${SYNC:-}"
GRACE_TIME="${GRACE_TIME:-10}"
LEASE_TIME="${LEASE_TIME:-10}"
THREADS="${THREADS:-8}"

# Logging function
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

# Error handling
error_exit() {
    log "ERROR: $1"
    exit 1
}

# Signal handlers for graceful shutdown
cleanup() {
    log "Shutting down NFS server gracefully..."

    # Unexport all filesystems
    if /usr/sbin/exportfs -uav 2>/dev/null; then
        log "Filesystems unexported successfully"
    fi

    # Stop NFS daemon with zero threads
    /usr/sbin/rpc.nfsd 0 2>/dev/null || true

    # Kill NFS processes
    for process in rpc.nfsd rpc.mountd rpcbind; do
        if pgrep "$process" >/dev/null 2>&1; then
            log "Stopping all instances of $process"

            # Send TERM signal to all instances
            pkill -TERM "$process" 2>/dev/null || true

            # Wait briefly for graceful shutdown
            sleep 1

            # Force kill any remaining instances
            if pgrep "$process" >/dev/null 2>&1; then
                log "Force killing remaining instances of $process"
                pkill -KILL "$process" 2>/dev/null || true
            fi
        fi
    done

    log "NFS server shutdown complete"
    exit 0
}

# Set up signal traps
trap cleanup SIGTERM SIGINT SIGQUIT

# Validate required environment variables
validate_config() {
    if [[ -z "$SHARED_DIRECTORY" ]]; then
        error_exit "SHARED_DIRECTORY environment variable must be set"
    fi

    if [[ ! -d "$SHARED_DIRECTORY" ]]; then
        error_exit "Shared directory '$SHARED_DIRECTORY' does not exist"
    fi
}

# Generate /etc/exports file
generate_exports() {
    local exports_file="/etc/exports"
    local ro_option="rw"
    local sync_option="async"

    # Set read-only option
    if [[ -n "${READ_ONLY}" ]]; then
        ro_option="ro"
    fi

    # Set sync option
    if [[ -n "${SYNC}" ]]; then
        sync_option="sync"
    fi

    # Create exports file
    cat > "$exports_file" << EOF
# NFS exports - generated automatically
$SHARED_DIRECTORY $PERMITTED($ro_option,fsid=0,$sync_option,no_subtree_check,no_auth_nlm,insecure,no_root_squash)
EOF

    log "Generated /etc/exports:"
    cat "$exports_file"
}

# Start RPC services
start_rpcbind() {
    log "Starting rpcbind..."
    /sbin/rpcbind -w -f &

    # Wait for rpcbind to be ready
    local retries=10
    while ! rpcinfo -p >/dev/null 2>&1 && [[ $retries -gt 0 ]]; do
        sleep 1
        ((retries--))
    done

    if [[ $retries -eq 0 ]]; then
        error_exit "rpcbind failed to start"
    fi

    log "rpcbind started successfully"
}

# Start NFS daemon
start_nfsd() {
    log "Starting NFS daemon with $THREADS threads, grace-time=$GRACE_TIME, lease-time=$LEASE_TIME"

    # Start NFS daemon with given parameters
    /usr/sbin/rpc.nfsd \
        --debug \
        --no-udp \
        --no-nfs-version 3 \
        --grace-time "$GRACE_TIME" \
        --lease-time "$LEASE_TIME" \
        "$THREADS"

    log "NFS daemon started successfully"
}

# Export filesystems
export_filesystems() {
    log "Exporting filesystems..."

    if /usr/sbin/exportfs -arv; then
        log "Filesystems exported successfully:"
        /usr/sbin/exportfs -v
    else
        error_exit "Failed to export filesystems"
    fi
}

# Start mount daemon
start_mountd() {
    log "Starting mount daemon..."

    /usr/sbin/rpc.mountd \
        --debug all \
        --no-udp \
        --no-nfs-version 3 \
        --port 20048

    # Verify mountd is running
    sleep 2
    if ! pidof rpc.mountd >/dev/null; then
        error_exit "Failed to start mount daemon"
    fi

    log "Mount daemon started successfully"
}

# Health check function
health_check() {
    local retries=5

    while [[ $retries -gt 0 ]]; do
        if pidof rpc.mountd >/dev/null; then
            return 0
        fi
        sleep 1
        ((retries--))
    done

    return 1
}

# Monitor NFS processes
monitor_processes() {
    while true; do
        if ! health_check; then
            log "NFS processes not healthy, exiting..."
            exit 1
        fi
        sleep 10
    done
}

# Main execution
main() {
    log "Starting NFS server..."
    log "Shared directory: $SHARED_DIRECTORY"
    log "Permitted clients: $PERMITTED"
    log "Read-only: ${READ_ONLY:-false}"
    log "Sync mode: ${SYNC:-false}"
    log "Grace time: ${GRACE_TIME}s"
    log "Lease time: ${LEASE_TIME}s"
    log "Threads: $THREADS"

    # Initialize
    validate_config
    generate_exports

    # Start services in order
    start_rpcbind
    start_nfsd
    export_filesystems
    start_mountd

    # Final health check
    if health_check; then
        log "NFS server startup complete and healthy"
    else
        error_exit "NFS server startup failed health check"
    fi

    # Monitor and keep running
    monitor_processes
}

# Run main function
main "$@"
