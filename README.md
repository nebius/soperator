# K8S operator for Slurm

### Documentation
The private doc targeted on solution architects is available here:  https://docs.nebius.dev/en/msp/slurm-operator/quickstart



### How to make a release

#### Step 1. Update version in file VERSION
The minor version depends on a person who is doing this. The remainder of dividing it by **4** should be equal to:
- @rodrijjke: **0**
- @dstaroff: **1**
- @pavel.sofrony: **2**
- @grigorii.rochev: **3**

#### Step 2. Sync the version among all other files
```
make sync-version
```

#### Step 3. Build & push container images
```
cd images && ./upload_to_build_agent.sh -u <ssh_user> -k <path_to_ssh_key>

ssh -i <path_to_ssh_key> <ssh_user>@195.242.25.163
sudo -i
cd /usr/src/prototypes/slurm/<ssh_user>

./build_common.sh
./build_all.sh &
./build_populate_jail.sh &

tail -n 1 outputs/*
```

#### Step 4. Update CRD
```
make manifests
```

#### Step 4. Build & push operator image
```
make docker-push
```

#### Step 5. Push helm charts
```
./release_helm.sh -afyr
```

#### Step 6. Pack new terraform tarball
```
./release_terraform.sh
```

#### Step 7. Check the release
Test your changes. After that, move the release tarball from `terraform-releases/unstable` directory to 
`terraform-releases/stable` and commit to trunk.
