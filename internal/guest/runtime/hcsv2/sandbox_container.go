// +build linux

package hcsv2

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/Microsoft/hcsshim/internal/guest/network"
	"github.com/Microsoft/hcsshim/internal/oc"
	oci "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"go.opencensus.io/trace"
)

func getSandboxRootDir(id string) string {
	return filepath.Join("/run/gcs/c", id)
}

func getSandboxMountsDir(id string) string {
	return filepath.Join(getSandboxRootDir(id), "sandboxMounts")
}

func getSandboxHostnamePath(id string) string {
	return filepath.Join(getSandboxRootDir(id), "hostname")
}

func getSandboxHostsPath(id string) string {
	return filepath.Join(getSandboxRootDir(id), "hosts")
}

func getSandboxResolvPath(id string) string {
	return filepath.Join(getSandboxRootDir(id), "resolv.conf")
}

func setupSandboxContainerSpec(ctx context.Context, id string, spec *oci.Spec) (err error) {
	ctx, span := trace.StartSpan(ctx, "hcsv2::setupSandboxContainerSpec")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()
	span.AddAttributes(trace.StringAttribute("cid", id))

	// Generate the sandbox root dir
	rootDir := getSandboxRootDir(id)
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return errors.Wrapf(err, "failed to create sandbox root directory %q", rootDir)
	}

	// Write the hostname
	hostname := spec.Hostname
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			return errors.Wrap(err, "failed to get hostname")
		}
	}

	sandboxHostnamePath := getSandboxHostnamePath(id)
	if err := ioutil.WriteFile(sandboxHostnamePath, []byte(hostname+"\n"), 0644); err != nil {
		return errors.Wrapf(err, "failed to write hostname to %q", sandboxHostnamePath)
	}

	// Write the hosts
	sandboxHostsContent := network.GenerateEtcHostsContent(ctx, hostname)
	sandboxHostsPath := getSandboxHostsPath(id)
	if err := ioutil.WriteFile(sandboxHostsPath, []byte(sandboxHostsContent), 0644); err != nil {
		return errors.Wrapf(err, "failed to write sandbox hosts to %q", sandboxHostsPath)
	}

	// Write resolv.conf
	ns, err := getNetworkNamespace(getNetworkNamespaceID(spec))
	if err != nil {
		return err
	}
	var searches, servers []string
	for _, n := range ns.Adapters() {
		if len(n.DNSSuffix) > 0 {
			searches = network.MergeValues(searches, strings.Split(n.DNSSuffix, ","))
		}
		if len(n.DNSServerList) > 0 {
			servers = network.MergeValues(servers, strings.Split(n.DNSServerList, ","))
		}
	}
	resolvContent, err := network.GenerateResolvConfContent(ctx, searches, servers, nil)
	if err != nil {
		return errors.Wrap(err, "failed to generate sandbox resolv.conf content")
	}
	sandboxResolvPath := getSandboxResolvPath(id)
	if err := ioutil.WriteFile(sandboxResolvPath, []byte(resolvContent), 0644); err != nil {
		return errors.Wrap(err, "failed to write sandbox resolv.conf")
	}

	if userstr, ok := spec.Annotations["io.microsoft.lcow.userstr"]; ok {
		if err := setUserStr(spec, userstr); err != nil {
			return err
		}
	}

	if rlimCore := spec.Annotations["io.microsoft.lcow.rlimitcore"]; rlimCore != "" {
		if err := setCoreRLimit(spec, rlimCore); err != nil {
			return err
		}
	}

	// TODO: JTERRY75 /dev/shm is not properly setup for LCOW I believe. CRI
	// also has a concept of a sandbox/shm file when the IPC NamespaceMode !=
	// NODE.

	// Force the parent cgroup into our /containers root
	spec.Linux.CgroupsPath = "/containers/" + id

	// Clear the windows section as we dont want to forward to runc
	spec.Windows = nil

	return nil
}
