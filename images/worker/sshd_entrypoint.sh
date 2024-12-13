#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Create privilege separation directory /var/run/sshd"
mkdir -p /var/run/sshd

echo "Waiting until munge started"
while [ ! -S "/run/munge/munge.socket.2" ]; do sleep 2; done

echo "Start sshd daemon"
/usr/sbin/sshd -D -e -f /mnt/ssh-configs/sshd_config
