# K8S operator for Slurm

### Documentation
The private doc targeted on solution architects is available here:  https://docs.nebius.dev/en/msp/slurm-operator/quickstart



### How to make a release

#### Step 1. Update version in file VERSION
After you started working on a new task and created a branch for it, patch the version in file [VERSION](./VERSION) by
adding the following suffix: `-<your_name>-<short_description>`.

For example, if the version looked so:
```
1.2.3
```
Make it look so:
```
1.2.3-rodrijjke-fix-munge
```
In this approach, the `MAJOR.MINOR.PATH` part that you remained untouched shows the version of the system your changes 
are based on.

#### Step 2. Release new version of all components
```
./release_all.sh -u <ssh_user_name> -k <path_to_private_ssh_key> -a <address_of_the_build_agent>
```
This will build & push all components (container images, operator, helm charts, etc.) and produce a terraform release 
tarball in the `terraform-releases/unstable` directory.

It will also unpack the tarball to the same directory, so you can apply it and check your changes.

#### Step 3. Create or update Slurm cluster
Enter the directory with your terraform files:
```
cd terraform-releases/unstable/terraform
```

In order to create or update a Slurm cluster, fill out the `terraform.tfvars` file.
There are some existing sets of variables that can be used for our test clusters located at 
[dev-tfvars](terraform-releases/unstable/dev-tfvars) directory.

Initialize & apply your terraform:
```
terraform init
terraform apply
```

#### Step 4. Check release
Test your changes. The general cluster functionality can be checked in the same way as we suggest it to our architects:
[How to check Slurm cluster](https://docs.nebius.dev/en/msp/slurm-operator/quickstart#how-to-check-the-created-slurm-cluster).

#### Step 5. (Optional) Fix found issues
If the initial version doesn't work, change your version suffix somehow. It's recommended just to add a counter to the 
end. In our example, the version could become `1.2.3-rodrijjke-fix-munge-1`. 

Then you need to create a new release (repeat [Step 2](#step-2-release-new-version-of-all-components)).

Don't worry about backing up your `terraform.tfvars` & `terraform.tfstate` files in the 
`terraform-releases/unstable/terraform` directory. Creation of a new release won't overwrite them.

#### Step 6. Make release stable
Create a PR in Arcanum with your changes and get review from the team.

When the review is completed, rebase your branch on trunk. Don't skip this step! During conflicts resolution, choose
the [VERSION](./VERSION) file content from trunk.

Change the [VERSION](./VERSION) file once again: increment the `MAJOR.MINOR.PATH` part following the semantic versioning
principle.

Create a final, stable, release by repeating [Step 2](#step-2-release-new-version-of-all-components) and pass the `-s`
option to `release_all.sh` script. It will put the new tarball to [stable](terraform-releases/stable) directory.

That's it!
