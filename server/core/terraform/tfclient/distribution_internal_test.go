package tfclient

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hashicorp/go-version"

	"github.com/runatlantis/atlantis/server/core/terraform"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

// Terraform and OpenTofu share version numbers; a cached Terraform 1.9.0
// must never be returned for OpenTofu 1.9.0.
func TestVersionCacheIsPerDistribution(t *testing.T) {
	v := version.Must(version.NewVersion("1.9.0"))
	versions := map[string]string{}
	var lock sync.Mutex
	tf, tofu := terraform.NewDistributionTerraform(), terraform.NewDistributionOpenTofu()

	setVersionBinaryPath(versions, &lock, tf, v, "/bin/terraform1.9.0")
	_, ok := getVersionBinaryPath(versions, &lock, tofu, v)
	Assert(t, !ok, "OpenTofu 1.9.0 must not resolve to the cached Terraform binary")
	p, ok := getVersionBinaryPath(versions, &lock, tf, v)
	Assert(t, ok, "Terraform 1.9.0 should be cached")
	Equals(t, "/bin/terraform1.9.0", p)
	Assert(t, getVersionOperationLock(map[string]*sync.Mutex{}, &lock, tf, v) != nil, "lock")
}

// A project on the distribution that is not the server default, with no
// version pinned or detected, gets that distribution's version from its
// binary on PATH instead of the server default's version.
func TestDefaultVersionForOtherDistribution(t *testing.T) {
	bin := t.TempDir()
	Ok(t, os.WriteFile(filepath.Join(bin, "tofu"), []byte("#!/bin/sh\necho 'OpenTofu v1.12.6'\n"), 0o755)) // #nosec G306 -- test executable
	t.Setenv("PATH", bin)

	c := &DefaultClient{distribution: terraform.NewDistributionTerraform(), defaultVersion: version.Must(version.NewVersion("1.16.3"))}
	log := logging.NewNoopLogger(t)

	got := c.DefaultVersionFor(log, terraform.NewDistributionOpenTofu())
	Assert(t, got != nil && got.String() == "1.12.6", "expected the tofu binary's version, got %v", got)
	// The server default distribution keeps using the default version.
	Assert(t, c.DefaultVersionFor(log, terraform.NewDistributionTerraform()) == nil, "terraform should fall back to the default version")
	Assert(t, c.DefaultVersionFor(log, nil) == nil, "no distribution should fall back to the default version")
}
