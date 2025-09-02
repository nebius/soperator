# NCCL-debug SPANK plugin

This SPANK plugin centralizes NCCL debug logs per job step-enforcing the log level you specify,
flexibly directing output to files or stdout, and integrating seamlessly with container environments.

## ⚙️ Configuration

You can configure the plugin three ways (in order of application):

0. [Plugin defaults]
1. Arguments from `plugstack.conf`
2. Environment variables
3. `srun` options

To see a list of arguments, run:
```shell
srun --help
```
And search for `Options provided by plugins`.
In that section you can find options prefixed with `nccld-`.

All settings provided locally are automatically propagated to `slurmstepd` on the worker nodes.

## ✋ Put on a handbrake

Control whether the plugin is active.

### Plugstack-wide default

In your `plugstack.conf`, you can turn the plugin on or off globally:

```conf
/usr/lib/x86_64-linux-gnu/slurm/spanknccldebug.so enabled=false
```

### On a per-job basis

#### Environment variable override

Set `SNCCLD_ENABLED=0` (or `false`) in your job’s environment to disable the plugin for that run,
even if it’s enabled in plugstack.
Conversely, `SNCCLD_ENABLED=1` (or `true`) forces it on.

```shell
export SNCCLD_ENABLED=0
srun …  # plugin will be skipped
```

#### `srun` option
    
Use `--nccld-enabled=0|1` on the command line to override both plugstack and environment.

## 📣 Log Level Management

Bypasses NCCL’s defaults and sets 
[`NCCL_DEBUG`](https://docs.nvidia.com/deeplearning/nccl/user-guide/docs/env.html#nccl-debug)
to your chosen verbosity via the plugin argument, `SNCCLD_LOG_LEVEL`, or `srun` option. 
Ensures every rank runs with the same level you requested.

## ✏️ Output Control

### Suppress stdout

`--nccld-out-stdout=0` (or `SNCCLD_OUT_STDOUT=0`) stops NCCL logs from going to your job’s standard output.

> [!IMPORTANT]
> If the user submitting a job already has env variable `NCCL_DEBUG` set to some value,
> outputting the NCCL logs to the stdout will be forced no matter the arguments.

### Toggle file output

`--nccld-out-file=0` (or `SNCCLD_OUT_FILE=0`) disables writing NCCL output to the dedicated files,
leaving only user-defined log file if specified.

### Output Directory

Use `--nccld-out-dir=/my/logs` to place NCCL output files enabled with `--nccld-out-file` under your chosen path.

> [!NOTE]
> The directory is created recursively if it doesn’t already exist.

### User-Defined Output File

If you set `NCCL_DEBUG_FILE=/path/to/log.out`, the plugin will:

1. Create the containing directory.
2. Tee NCCL output into both your file and the plugin’s aggregated logs.

> [!NOTE]
> The file is created if it doesn’t already exist.

## 🚢 Container Support

Automatically emits **Enroot** mount configurations so that your debug directory is bind-mounted inside the container
before it launches.

> [!IMPORTANT]
> No manual `--container-mounts` flags needed - logs just work in both native and containerized Slurm jobs.

## 📋 TODOs

- Expansion of placeholders in user-provided
  [`NCCL_DEBUG_FILE`](https://docs.nvidia.com/deeplearning/nccl/user-guide/docs/env.html#nccl-debug-file)
  [#1021](https://github.com/nebius/soperator/issues/1021)
- Log rotation & cleanup
  [#1022](https://github.com/nebius/soperator/issues/1022)
- String deduplication
  [#1032](https://github.com/nebius/soperator/issues/1032)
- Separation of argument handling & config management
  [#1033](https://github.com/nebius/soperator/issues/1033)

---

## :construction: Development

There are 3 Dockerfiles in the [./docker](./docker) directory:

1. `base` - Fedora environment for building Slurm from sources.
2. `builder` - To build shared library from sources.
3. `headers` - To collect ready to use Slurm and SPANK header files.

### Slurm Includes

In order to get Slurm headers for the IntelliSense & Co., run:

```shell
make headers
```

This will build Slurm from sources, and put its headers into the [./vendor/slurm](./vendor) directory.

> [!TIP]
> You can use needed version of Slurm via
```shell
make headers SLURM_VERSION=<SLURM_VERSION>
```

### Building a shared library

Builder supports:

- Different architectures:

    ```shell
    make build ARCH=<ARCH>
    ```

  - For `x64` use `amd64`.
  - For **ARM** use `arm64`.

- Target modes:

    ```shell
    make build TARGET_COMPILATION_MODE=<MODE>
    ```
  
  - For **debug** use `debug`.
  - For **release** use `release`.

> [!TIP]
> You can use needed version of Slurm via
```shell
make build SLURM_VERSION=<SLURM_VERSION>
```

### Deployment on Soperator cluster

#### 1. Cluster configuration

Make sure your cluster has `spec.plugStackConfig.ncclDebug.enabled` set to `true`.

#### 2. Build

Once changes are made, you can rebuild the shared library to get a fresh `spanknccldebug.so` file in 
[./build](./build) directory with:

```shell
make build <PARAMS>
```

#### 3. kubectl 

Make sure you have **kubectl** using needed cluster by default.

> [!WARNING]
> Changes will be performed in the current context.

#### 4. Redeploy

Run `make redeploy` to copy the plugin's `.so` file onto the cluster pods.

> [!TIP]
> You can set `NODE_COUNT_WORKER` and `NODE_COUNT_LOGIN` to specify a number of worker and login pods on the cluster.

```shell
make redeploy NODE_COUNT_WORKER=8 NODE_COUNT_LOGIN=3
```
