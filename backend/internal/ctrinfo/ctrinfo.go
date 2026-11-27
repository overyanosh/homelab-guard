// Package ctrinfo centralises the small helpers describing a container: engine
// detection, attached networks and published ports. It is shared by the server
// and the remote agent so both report the same shape to the UI.
package ctrinfo

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

// RuntimeName identifies the container engine behind the Docker-compatible
// socket. Podman reports a "Podman Engine" component in /version, Docker reports
// "Engine"/"Docker Engine".
func RuntimeName(cli *client.Client) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if v, err := cli.ServerVersion(ctx); err == nil {
		for _, c := range v.Components {
			if strings.Contains(strings.ToLower(c.Name), "podman") {
				return "podman"
			}
		}
		if strings.Contains(strings.ToLower(v.Platform.Name), "podman") {
			return "podman"
		}
		for _, c := range v.Components {
			if strings.Contains(strings.ToLower(c.Name), "engine") {
				return "docker"
			}
		}
	}
	if info, err := cli.Info(ctx); err == nil {
		hay := strings.ToLower(info.Name + " " + info.OperatingSystem + " " + info.ServerVersion)
		if strings.Contains(hay, "podman") {
			return "podman"
		}
	}
	return "docker"
}

// NetworkNames returns the networks a container is attached to. With rootless
// Podman the Docker-compatible API leaves NetworkSettings.Networks empty, so the
// network mode (pasta, bridge, host, custom network, ...) is used instead.
func NetworkNames(insp dockerTypes.ContainerJSON) []string {
	names := []string{}
	if insp.NetworkSettings != nil {
		for name := range insp.NetworkSettings.Networks {
			names = append(names, name)
		}
	}
	if len(names) == 0 && insp.HostConfig != nil {
		if mode := strings.TrimSpace(string(insp.HostConfig.NetworkMode)); mode != "" {
			names = append(names, mode)
		}
	}
	sort.Strings(names)
	return names
}

// PortNames returns human-readable port mappings, e.g. "8080:80/tcp" for
// published ports and "9015/tcp" for merely exposed ones.
func PortNames(insp dockerTypes.ContainerJSON) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	if insp.NetworkSettings != nil {
		for port, bindings := range insp.NetworkSettings.Ports {
			if len(bindings) == 0 {
				add(port.Port() + "/" + port.Proto())
				continue
			}
			for _, b := range bindings {
				if b.HostPort != "" {
					add(b.HostPort + ":" + port.Port() + "/" + port.Proto())
					seen[port.Port()+"/"+port.Proto()] = true // published: hide bare form
				} else {
					add(port.Port() + "/" + port.Proto())
				}
			}
		}
	}
	if insp.Config != nil {
		for port := range insp.Config.ExposedPorts {
			add(port.Port() + "/" + port.Proto())
		}
	}
	sort.Strings(out)
	return out
}

// ShortID returns the 12-character container ID.
func ShortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// PortNumber is a small helper for composing port strings in tests/tools.
func PortNumber(n uint16) string { return strconv.Itoa(int(n)) }
