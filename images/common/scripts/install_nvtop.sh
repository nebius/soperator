#!/bin/bash

# Download, compile and install nvtop monitoring utility
cd /usr/src && \
    wget https://github.com/Syllo/nvtop/archive/refs/tags/3.1.0.tar.gz && \
    tar -xzvf 3.1.0.tar.gz && \
    rm 3.1.0.tar.gz && \
    cd nvtop-3.1.0 && \
    mkdir build &&
    cd build && \
    mkdir cmake && \
    cd cmake && \
    wget https://github.com/Kitware/CMake/releases/download/v3.30.1/cmake-3.30.1-linux-x86_64.sh && \
    chmod +x cmake-3.30.1-linux-x86_64.sh && \
    ./cmake-3.30.1-linux-x86_64.sh --skip-license && \
    cd .. && \
    ./cmake/bin/cmake .. -DNVIDIA_SUPPORT=ON -DAMDGPU_SUPPORT=OFF -DINTEL_SUPPORT=OFF -DMSM_SUPPORT=OFF -DCMAKE_C_FLAGS="-I/usr/include/drm" && \
    make && \
    make install && \
    cd ../.. && \
    rm -rf nvtop-3.1.0
