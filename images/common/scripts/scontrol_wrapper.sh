#!/bin/bash

# Wrapper that takes the place of /usr/bin/scontrol inside the jail. It warns on stderr about
# configuration changes that Soperator does not persist and then runs the real binary.
# The warning is printed for scripts and AI agents too, because they issue such commands on behalf
# of users; set SOPERATOR_SCONTROL_QUIET=1 to silence it, stdout and the exit code never change.

REAL=/usr/bin/scontrol.real

# Returns 0 when the arguments describe a change that lives only in slurmctld memory:
# partition create/update/delete, or node features, GRES and weight updates.
needs_warning() {
    # scontrol ignores case in subcommands and parameter names, so the matching below must too.
    shopt -s nocasematch

    local subcommand=""
    while [ $# -gt 0 ]; do
        case "$1" in
            -M|--clusters|--cluster)
                shift 2
                continue
                ;;
            -*)
                shift
                continue
                ;;
        esac
        subcommand="$1"
        shift
        break
    done

    # scontrol accepts abbreviated subcommands: "create" needs at least two characters,
    # "delete" and "update" are recognized from a single one.
    local command=""
    case "$subcommand" in
        "") return 1 ;;
        d*) [[ "delete" == "$subcommand"* ]] && command="delete" ;;
        u*) [[ "update" == "$subcommand"* ]] && command="update" ;;
        c?*) [[ "create" == "$subcommand"* ]] && command="create" ;;
    esac
    [ -n "$command" ] || return 1

    local partition=0 node=0 node_attribute=0 arg
    for arg in "$@"; do
        case "$arg" in
            # Any partition change is dropped at the next reconfigure, for example:
            #   scontrol create PartitionName=spot Nodes=worker-[0-15] PriorityTier=1
            #   scontrol update PartitionName=main MaxTime=2-00:00:00
            #   scontrol delete PartitionName=spot
            partitionname=*) partition=1 ;;

            # A node update is non-persistent only when it touches attributes that come from the
            # NodeName= line of slurm.conf; State=, Reason= and Comment= are saved in
            # StateSaveLocation and survive reconfigure and restart.
            nodename=*) node=1 ;;

            # These attributes are checked together with NodeName= because jobs and reservations
            # accept Features= and Gres= as well (scontrol update JobId=5 Features=h100), and
            # those updates are persistent. Features= is the scontrol alias of AvailableFeatures=.
            # Examples:
            #   scontrol update NodeName=worker-0 Features=h100,ib
            #   scontrol update NodeName=worker-0 ActiveFeatures=h100
            #   scontrol update NodeName=worker-[0-7] Gres=gpu:8
            #   scontrol update NodeName=worker-0 Weight=10
            availablefeatures=*|activefeatures=*|features=*|gres=*|weight=*) node_attribute=1 ;;
        esac
    done

    [ "$partition" -eq 1 ] && return 0
    [ "$command" = "update" ] && [ "$node" -eq 1 ] && [ "$node_attribute" -eq 1 ] && return 0
    return 1
}

print_warning() {
    cat >&2 <<'EOF'
WARNING: this scontrol change is not persistent in Soperator.
Slurm configuration (slurm.conf, partitions, node features) is rendered from the SlurmCluster
resource and rewritten into the shared root on every reconciliation. The change you are making lives
only in slurmctld memory and is lost on the next reconfigure, slurmctld restart or configuration
update.
To make it permanent, change the SlurmCluster spec (partitionConfiguration, customSlurmConfig) or
whatever manages that spec: Helm, Flux, Terraform or your service provider.
See https://github.com/nebius/soperator/blob/main/docs/slurm-configuration.md
Set SOPERATOR_SCONTROL_QUIET=1 to suppress this warning.

EOF
}

main() {
    if [ "${SOPERATOR_SCONTROL_QUIET:-}" != "1" ] && needs_warning "$@"; then
        print_warning
    fi
    exec "$REAL" "$@"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
