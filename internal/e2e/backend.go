package e2e

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

const terraformBackendOverrideFile = "terraform_backend_override.tf"

var terraformBackendS3EndpointRe = regexp.MustCompile(`(?m)^([ \t]*s3[ \t]*=[ \t]*)"[^"\r\n]*"([ \t]*)$`)

func overrideTerraformBackendS3Endpoint(workdir, endpoint string) error {
	if endpoint == "" {
		return nil
	}

	path := filepath.Join(workdir, terraformBackendOverrideFile)
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Terraform backend override %s: %w", path, err)
	}

	matches := terraformBackendS3EndpointRe.FindAllSubmatchIndex(contents, -1)
	if len(matches) != 1 {
		return fmt.Errorf("find exactly one S3 endpoint in Terraform backend override %s: found %d", path, len(matches))
	}

	match := matches[0]
	updated := make([]byte, 0, len(contents)+len(endpoint))
	updated = append(updated, contents[:match[0]]...)
	updated = append(updated, contents[match[2]:match[3]]...)
	updated = strconv.AppendQuote(updated, endpoint)
	updated = append(updated, contents[match[4]:match[5]]...)
	updated = append(updated, contents[match[1]:]...)

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat Terraform backend override %s: %w", path, err)
	}
	if err := os.WriteFile(path, updated, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write Terraform backend override %s: %w", path, err)
	}
	log.Printf("Terraform backend S3 endpoint overridden with %s", endpoint)
	return nil
}
