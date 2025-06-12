FROM spanknccldebug-base AS header-copier

RUN mkdir -p /tmp/include
CMD ["cp", "-R", "/slurm/slurm", "/tmp/include/"]
