#!/bin/bash

# Install munge
apt update && apt install -y munge libmunge-dev

# Fix permissions
chmod -R 700 /etc/munge /var/log/munge
chmod -R 711 /var/lib/munge
chown -R 0:0 /etc/munge /var/log/munge /var/lib/munge
