// Package composegen generates a docker-compose service definition from a
// container inspection. Shared by the server and the remote agent.
package composegen

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	dockertypes "github.com/docker/docker/api/types"
)

// FromInspect builds a best-effort docker-compose service definition.
func FromInspect(c dockertypes.ContainerJSON) string { return composeFromInspect(c) }

// DetectEnvFile reads the .env sitting next to a compose project's config file.
func DetectEnvFile(c dockertypes.ContainerJSON) string { return detectComposeEnv(c) }

// isNoisyLabel filters labels added by build tools and base images, which are
// not part of the service definition the user wrote.
func isNoisyLabel(key string) bool {
	switch {
	case key == "io.buildah.version",
		key == "io.container.manager",
		strings.HasPrefix(key, "org.opencontainers.image."),
		strings.HasPrefix(key, "org.systemd.property."),
		strings.HasPrefix(key, "io.buildkit."):
		return true
	}
	return false
}

// isSensitiveEnv reports whether an environment variable name is likely to hold
// a secret, so its value is masked when generating a compose preview.
func isSensitiveEnv(name string) bool {
	u := strings.ToUpper(name)
	for _, s := range []string{"KEY", "SECRET", "TOKEN", "PASSWORD", "PASSWD", "PWD"} {
		if strings.Contains(u, s) {
			return true
		}
	}
	return false
}

// detectComposeEnv tries to read a .env file sitting next to a compose project's
// config file, based on the container's compose labels. Returns "" when not found.
func detectComposeEnv(c dockertypes.ContainerJSON) string {
	if c.Config == nil || c.Config.Labels == nil {
		return ""
	}
	dir := c.Config.Labels["com.docker.compose.project.working_dir"]
	if dir == "" {
		if cf := c.Config.Labels["com.docker.compose.project.config_files"]; cf != "" {
			first := strings.Split(cf, ",")[0]
			dir = filepath.Dir(first)
		}
	}
	if dir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		return ""
	}
	return string(b)
}

// composeFromInspect builds a best-effort docker-compose service definition from
// a container inspection (image, restart policy, ports, env, mounts).
func composeFromInspect(c dockertypes.ContainerJSON) string {
	service := strings.TrimPrefix(c.Name, "/")
	if service == "" {
		service = c.ID
		if len(service) > 12 {
			service = service[:12]
		}
	}

	var b strings.Builder
	b.WriteString("services:\n")
	b.WriteString("  " + service + ":\n")
	if c.Config != nil && c.Config.Image != "" {
		b.WriteString("    image: " + c.Config.Image + "\n")
	}
	b.WriteString("    container_name: " + service + "\n")
	if c.HostConfig != nil && c.HostConfig.RestartPolicy.Name != "" {
		b.WriteString("    restart: " + string(c.HostConfig.RestartPolicy.Name) + "\n")
	}

	if c.NetworkSettings != nil && len(c.NetworkSettings.Ports) > 0 {
		b.WriteString("    ports:\n")
		for port, bindings := range c.NetworkSettings.Ports {
			if len(bindings) == 0 {
				b.WriteString("      - \"" + port.Port() + "\"\n")
				continue
			}
			for _, bnd := range bindings {
				host := ""
				if bnd.HostIP != "" && bnd.HostIP != "0.0.0.0" && bnd.HostIP != "::" {
					host = bnd.HostIP + ":"
				}
				if bnd.HostPort != "" {
					b.WriteString("      - \"" + host + bnd.HostPort + ":" + port.Port() + "\"\n")
				} else {
					b.WriteString("      - \"" + port.Port() + "\"\n")
				}
			}
		}
	}

	if c.Config != nil && len(c.Config.Env) > 0 {
		var envs []string
		for _, e := range c.Config.Env {
			name := e
			val := ""
			if i := strings.IndexByte(e, '='); i >= 0 {
				name = e[:i]
				val = e[i+1:]
			}
			switch name {
			case "PATH", "HOSTNAME", "HOME", "TERM":
				continue
			}
			if isSensitiveEnv(name) && val != "" {
				val = "***"
			}
			envs = append(envs, "      - \""+name+"="+val+"\"")
		}
		if len(envs) > 0 {
			b.WriteString("    environment:\n")
			b.WriteString(strings.Join(envs, "\n") + "\n")
		}
	}

	// Labels (guard.*, compose metadata, user labels) are part of the container
	// definition and must survive a regenerate/apply cycle. Tooling noise from
	// the base image is skipped to keep the file readable.
	if c.Config != nil && len(c.Config.Labels) > 0 {
		keys := make([]string, 0, len(c.Config.Labels))
		for k := range c.Config.Labels {
			if isNoisyLabel(k) {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			b.WriteString("    labels:\n")
			for _, k := range keys {
				b.WriteString("      - \"" + k + "=" + c.Config.Labels[k] + "\"\n")
			}
		}
	}

	// Command / entrypoint, when explicitly set on the container.
	if c.Config != nil && len(c.Config.Cmd) > 0 {
		b.WriteString("    command: [")
		for i, arg := range c.Config.Cmd {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(arg))
		}
		b.WriteString("]\n")
	}
	if c.Config != nil && len(c.Config.Entrypoint) > 0 {
		b.WriteString("    entrypoint: [")
		for i, arg := range c.Config.Entrypoint {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(arg))
		}
		b.WriteString("]\n")
	}

	if len(c.Mounts) > 0 {
		var vols []string
		for _, m := range c.Mounts {
			switch m.Type {
			case "bind":
				vols = append(vols, "      - \""+m.Source+":"+m.Destination+"\"")
			case "volume":
				vols = append(vols, "      - \""+m.Name+":"+m.Destination+"\"")
			}
		}
		if len(vols) > 0 {
			b.WriteString("    volumes:\n")
			b.WriteString(strings.Join(vols, "\n") + "\n")
		}
	}

	return b.String()
}
