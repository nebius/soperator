#!/bin/bash

export PMIX_VERSION=5.0.2
cd /usr/src && \
    wget https://github.com/openpmix/openpmix/releases/download/v${PMIX_VERSION}/pmix-${PMIX_VERSION}.tar.gz && \
    tar -xzvf pmix-${PMIX_VERSION}.tar.gz && \
    rm -rf pmix-${PMIX_VERSION}.tar.gz && \
    cd /usr/src/pmix-${PMIX_VERSION} && \
    ./configure && \
    make -j$(nproc) && \
    make install
