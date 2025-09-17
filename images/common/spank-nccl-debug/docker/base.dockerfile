# Or arm64v8
ARG ARCH="amd64"

ARG SLURM_VERSION="25-05-3-1"

FROM ${ARCH}/fedora:42 AS slurm-base
LABEL org.opencontainers.image.authors="Dmitrii Starov <dstaroff@nebius.com>"

RUN dnf install -y \
        dnf-utils \
        util-linux \
        dbus-devel \
        procps-ng \
        hwinfo \
        git \
        gcc \
        gawk \
        jq \
        munge \
        pmix-devel

ARG SLURM_VERSION
RUN git clone \
        --depth 1 \
        --branch slurm-${SLURM_VERSION} \
        https://github.com/SchedMD/slurm.git && \
    cd slurm && \
    yum-builddep -y slurm.spec && \
    ./configure \
        --prefix=/usr --program-prefix= --exec-prefix=/usr \
        --bindir=/usr/bin --sbindir=/usr/sbin \
        --sysconfdir=/etc/slurm \
        --includedir=/usr/include --libdir=/usr/lib64 --libexecdir=/usr/libexec \
        --localstatedir=/var --sharedstatedir=/var/lib --runstatedir=/run \
        --datadir=/usr/share --mandir=/usr/share/man --infodir=/usr/share/info \
        --with-bpf \
        --with-pmix && \
    make -j && \
    make install && \
    make clean
