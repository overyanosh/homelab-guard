package updater

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"gopkg.in/yaml.v3"
)

// ComposeService is the subset of a docker-compose service definition that the
// apply engine understands.
type ComposeService struct {
	Service       string
	Image         string
	ContainerName string
	Restart       string
	Ports         []string
	Environment   map[string]string
	Volumes       []string
	Command       []string
	NetworkMode   string
	Labels        map[string]string
}

// ApplyResult reports the outcome of applying a compose definition.
type ApplyResult struct {
	ContainerName string `json:"container_name"`
	Image         string `json:"image"`
	Status        string `json:"status"` // applied | rolled-back
	Message       string `json:"message,omitempty"`
	Logs          string `json:"logs,omitempty"`
	AIDiagnosis   string `json:"ai_diagnosis,omitempty"`
}

var (
	envBracedRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)
	envPlainRe  = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
)

// ParseDotEnv parses KEY=VALUE lines (ignoring comments and blank lines).
func ParseDotEnv(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		out[key] = val
	}
	return out
}

// substituteEnv expands ${VAR}, ${VAR:-default} and $VAR using vars.
func substituteEnv(s string, vars map[string]string) string {
	s = envBracedRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := envBracedRe.FindStringSubmatch(m)
		if v, ok := vars[sub[1]]; ok {
			return v
		}
		return sub[2]
	})
	return envPlainRe.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.TrimPrefix(m, "$")
		if v, ok := vars[name]; ok {
			return v
		}
		return m
	})
}

// ParseComposeService extracts a single service definition from a compose YAML
// document, expanding ${VAR} references using vars. The service is resolved by:
// exact key, then a matching container_name, then the only declared service.
func ParseComposeService(doc, service string, vars map[string]string) (*ComposeService, error) {
	var parsed struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(doc), &parsed); err != nil {
		return nil, fmt.Errorf("invalid compose YAML: %w", err)
	}
	if len(parsed.Services) == 0 {
		return nil, fmt.Errorf("compose file declares no services")
	}

	names := make([]string, 0, len(parsed.Services))
	for name := range parsed.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	rawSvc, ok := parsed.Services[service]
	if !ok {
		// A service may declare container_name different from its key.
		for _, name := range names {
			if cn, _ := parsed.Services[name]["container_name"].(string); strings.TrimSpace(cn) == service {
				service, rawSvc, ok = name, parsed.Services[name], true
				break
			}
		}
	}
	if !ok {
		// Without an explicit choice, a single-service file is unambiguous.
		if len(parsed.Services) == 1 {
			service = names[0]
			rawSvc = parsed.Services[service]
		} else {
			return nil, fmt.Errorf(
				"service %q introuvable — services disponibles : %s. Indiquez le service à déployer.",
				service, strings.Join(names, ", "),
			)
		}
	}

	svc := &ComposeService{
		Service:     service,
		Environment: map[string]string{},
		Labels:      map[string]string{},
	}

	svc.Image = substituteEnv(strVal(rawSvc["image"]), vars)
	svc.ContainerName = substituteEnv(strVal(rawSvc["container_name"]), vars)
	svc.Restart = strVal(rawSvc["restart"])
	svc.NetworkMode = strVal(rawSvc["network_mode"])

	for _, p := range listVal(rawSvc["ports"]) {
		svc.Ports = append(svc.Ports, substituteEnv(p, vars))
	}
	for _, v := range listVal(rawSvc["volumes"]) {
		svc.Volumes = append(svc.Volumes, substituteEnv(v, vars))
	}
	for k, v := range mapVal(rawSvc["environment"]) {
		svc.Environment[k] = substituteEnv(v, vars)
	}
	for k, v := range mapVal(rawSvc["labels"]) {
		svc.Labels[k] = substituteEnv(v, vars)
	}
	switch cmd := rawSvc["command"].(type) {
	case string:
		svc.Command = []string{substituteEnv(cmd, vars)}
	case []any:
		for _, c := range cmd {
			svc.Command = append(svc.Command, substituteEnv(strVal(c), vars))
		}
	}

	return svc, nil
}

func strVal(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// listVal flattens a compose list that may mix scalars and maps.
func listVal(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			// Long syntax (e.g. ports/volumes): keep a simple textual form.
			switch {
			case m["target"] != nil && m["published"] != nil:
				proto := strVal(m["protocol"])
				if proto == "" {
					proto = "tcp"
				}
				s := strVal(m["published"]) + ":" + strVal(m["target"]) + "/" + proto
				if ip := strVal(m["host_ip"]); ip != "" {
					s = ip + ":" + s
				}
				out = append(out, s)
			case m["source"] != nil && m["target"] != nil:
				s := strVal(m["source"]) + ":" + strVal(m["target"])
				if r, ok := m["read_only"].(bool); ok && r {
					s += ":ro"
				}
				out = append(out, s)
			}
			continue
		}
		out = append(out, strVal(item))
	}
	return out
}

// mapVal normalises both map and "K=V" list forms of environment/labels.
func mapVal(v any) map[string]string {
	out := map[string]string{}
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			out[k] = strVal(val)
		}
	case []any:
		for _, item := range t {
			s := strVal(item)
			if i := strings.IndexByte(s, '='); i > 0 {
				out[s[:i]] = s[i+1:]
			} else if s != "" {
				out[s] = ""
			}
		}
	}
	return out
}

// parsePort parses "80", "8080:80", "127.0.0.1:8080:80/udp".
func parsePort(spec string) (nat.Port, nat.PortBinding, error) {
	proto := "tcp"
	if i := strings.LastIndexByte(spec, '/'); i > 0 {
		proto = strings.ToLower(spec[i+1:])
		spec = spec[:i]
	}
	parts := strings.Split(spec, ":")
	var containerPort, hostPort, hostIP string
	switch len(parts) {
	case 1:
		containerPort = parts[0]
	case 2:
		hostPort, containerPort = parts[0], parts[1]
	case 3:
		hostIP, hostPort, containerPort = parts[0], parts[1], parts[2]
	default:
		return "", nat.PortBinding{}, fmt.Errorf("invalid port mapping %q", spec)
	}
	if _, err := strconv.Atoi(containerPort); err != nil {
		return "", nat.PortBinding{}, fmt.Errorf("invalid container port in %q", spec)
	}
	port, err := nat.NewPort(proto, containerPort)
	if err != nil {
		return "", nat.PortBinding{}, err
	}
	return port, nat.PortBinding{HostIP: hostIP, HostPort: hostPort}, nil
}

// ApplyCompose recreates containerID using the edited compose definition. The
// original container is kept under a backup name until the new one is running
// (and healthy, when a healthcheck exists); otherwise it is restored.
func (e *Engine) ApplyCompose(ctx context.Context, containerID, service, composeDoc, dotEnv string) (*ApplyResult, error) {
	vars := ParseDotEnv(dotEnv)
	svc, err := ParseComposeService(composeDoc, service, vars)
	if err != nil {
		return nil, err
	}

	insp, err := e.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspect failed: %w", err)
	}
	name := strings.TrimPrefix(insp.Name, "/")

	// Never recreate the container that is serving this very request.
	if self, err := os.Hostname(); err == nil && self != "" && strings.HasPrefix(insp.ID, self) {
		return nil, fmt.Errorf("refusing to recreate the Homelab Guard container itself (%s); apply from a remote agent instead", name)
	}

	imageRef := svc.Image
	if imageRef == "" && insp.Config != nil {
		imageRef = insp.Config.Image
	}
	if imageRef == "" {
		return nil, fmt.Errorf("compose definition has no image")
	}

	// Best-effort pull so "apply" picks up the latest tag; ignore failures
	// (offline hosts must still be able to recreate from the local image).
	pullCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	if rc, err := e.docker.ImagePull(pullCtx, imageRef, image.PullOptions{}); err == nil {
		_, _ = io.Copy(io.Discard, rc)
		_ = rc.Close()
	}
	cancel()

	newCfg, newHC, netCfg, err := buildConfig(insp, svc, imageRef)
	if err != nil {
		return nil, err
	}

	backupName := fmt.Sprintf("%s__guard_backup_%d", name, time.Now().Unix())
	if err := e.docker.ContainerRename(ctx, insp.ID, backupName); err != nil {
		return nil, fmt.Errorf("failed to rename current container: %w", err)
	}

	stopTimeout := 15
	_ = e.docker.ContainerStop(ctx, insp.ID, container.StopOptions{Timeout: &stopTimeout})

	created, err := e.docker.ContainerCreate(ctx, newCfg, newHC, netCfg, nil, name)
	if err != nil {
		e.restoreBackup(insp.ID, backupName, name)
		return nil, fmt.Errorf("failed to create new container (previous container restored): %w", err)
	}

	if err := e.docker.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		e.rollback(ctx, created.ID, insp.ID, backupName, name)
		result := &ApplyResult{
			ContainerName: name,
			Image:         imageRef,
			Status:        "rolled-back",
			Message:       fmt.Sprintf("new container failed to start: %v", err),
		}
		result.Logs = e.collectContainerLogs(ctx, created.ID)
		result.AIDiagnosis = e.requestAIDiagnosis(name, "compose apply failed to start", result.Logs)
		return result, nil
	}

	if reason := e.waitUntilHealthy(ctx, created.ID, svc.Environment); reason != "" {
		logs := e.collectContainerLogs(ctx, created.ID)
		e.rollback(ctx, created.ID, insp.ID, backupName, name)
		return &ApplyResult{
			ContainerName: name,
			Image:         imageRef,
			Status:        "rolled-back",
			Message:       reason,
			Logs:          logs,
			AIDiagnosis:   e.requestAIDiagnosis(name, "compose apply failed health check", logs),
		}, nil
	}

	// Success: drop the backup.
	force := true
	_ = e.docker.ContainerRemove(context.Background(), insp.ID, container.RemoveOptions{Force: force})

	return &ApplyResult{
		ContainerName: name,
		Image:         imageRef,
		Status:        "applied",
		Message:       "container recreated from compose definition",
	}, nil
}

// CreateFromCompose creates and starts a brand-new container from a compose
// service definition. On failure the half-created container is removed.
func (e *Engine) CreateFromCompose(ctx context.Context, name, service, composeDoc, dotEnv string) (*ApplyResult, error) {
	vars := ParseDotEnv(dotEnv)
	if strings.TrimSpace(service) == "" {
		service = name
	}
	svc, err := ParseComposeService(composeDoc, service, vars)
	if err != nil {
		return nil, err
	}

	containerName := strings.TrimPrefix(strings.TrimSpace(name), "/")
	if containerName == "" {
		containerName = strings.TrimPrefix(svc.ContainerName, "/")
	}
	if !validContainerName(containerName) {
		return nil, fmt.Errorf("invalid container name %q (allowed: letters, digits, '_', '.', '-')", containerName)
	}
	if strings.TrimSpace(svc.Image) == "" {
		return nil, fmt.Errorf("compose definition has no image")
	}

	log.Printf("compose create: name=%q service=%q image=%q", containerName, service, svc.Image)

	// Fail early with an actionable message when the name is already taken.
	if existing, err := e.docker.ContainerList(ctx, container.ListOptions{All: true}); err == nil {
		for _, c := range existing {
			for _, n := range c.Names {
				if strings.TrimPrefix(n, "/") == containerName {
					return nil, fmt.Errorf(
						"un conteneur nommé %q existe déjà (id %s) : choisissez un autre nom ou supprimez-le d'abord",
						containerName, c.ID[:12],
					)
				}
			}
		}
	}

	// Best-effort pull so a missing image can still be created.
	pullCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	if rc, err := e.docker.ImagePull(pullCtx, svc.Image, image.PullOptions{}); err == nil {
		_, _ = io.Copy(io.Discard, rc)
		_ = rc.Close()
	}
	cancel()

	cfg, hc, netCfg, err := buildConfig(dockerTypes.ContainerJSON{}, svc, svc.Image)
	if err != nil {
		return nil, err
	}
	cfg.Hostname = ""

	created, err := e.docker.ContainerCreate(ctx, cfg, hc, netCfg, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	force := true
	if err := e.docker.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		logs := e.collectContainerLogs(ctx, created.ID)
		_ = e.docker.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: force})
		return &ApplyResult{
			ContainerName: containerName,
			Image:         svc.Image,
			Status:        "failed",
			Message:       fmt.Sprintf("container failed to start: %v", err),
			Logs:          logs,
			AIDiagnosis:   e.requestAIDiagnosis(containerName, "compose create failed to start", logs),
		}, nil
	}

	if reason := e.waitUntilHealthy(ctx, created.ID, svc.Environment); reason != "" {
		logs := e.collectContainerLogs(ctx, created.ID)
		_ = e.docker.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: force})
		return &ApplyResult{
			ContainerName: containerName,
			Image:         svc.Image,
			Status:        "failed",
			Message:       reason,
			Logs:          logs,
			AIDiagnosis:   e.requestAIDiagnosis(containerName, "compose create failed health check", logs),
		}, nil
	}

	return &ApplyResult{
		ContainerName: containerName,
		Image:         svc.Image,
		Status:        "created",
		Message:       "container created and started",
	}, nil
}

var containerNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func validContainerName(name string) bool {
	return containerNameRe.MatchString(name)
}

// buildConfig derives Config/HostConfig from the inspected container, overridden
// by the compose service fields.
func buildConfig(insp dockerTypes.ContainerJSON, svc *ComposeService, imageRef string) (*container.Config, *container.HostConfig, *network.NetworkingConfig, error) {
	// ContainerJSON embeds *ContainerJSONBase: when building from scratch (new
	// container) it is nil, so make sure the embedded struct exists.
	if insp.ContainerJSONBase == nil {
		insp.ContainerJSONBase = &dockerTypes.ContainerJSONBase{}
	}

	cfg := container.Config{Image: imageRef}
	if insp.Config != nil {
		cfg = *insp.Config
		cfg.Image = imageRef
	}

	hc := container.HostConfig{}
	if insp.HostConfig != nil {
		hc = *insp.HostConfig
	}
	// Podman reports these cgroup-v1-only knobs (often as explicit zero values).
	// Re-sending them makes crun fail on cgroup v2 ("cannot set memory
	// swappiness with cgroupv2"), so drop them when recreating the container.
	hc.Resources.MemorySwappiness = nil
	hc.Resources.OomKillDisable = nil

	if svc.ContainerName != "" {
		// Kept for parity with compose semantics; the engine uses the existing name.
		cfg.Hostname = ""
	}
	if svc.Restart != "" {
		hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(svc.Restart)}
	} else {
		hc.RestartPolicy = container.RestartPolicy{}
	}
	if svc.NetworkMode != "" {
		hc.NetworkMode = container.NetworkMode(svc.NetworkMode)
	}
	if svc.Command != nil {
		cfg.Cmd = svc.Command
	}

	// Ports
	exposed := nat.PortSet{}
	bindings := nat.PortMap{}
	for _, spec := range svc.Ports {
		port, binding, err := parsePort(spec)
		if err != nil {
			return nil, nil, nil, err
		}
		exposed[port] = struct{}{}
		if binding.HostPort != "" || binding.HostIP != "" {
			bindings[port] = append(bindings[port], binding)
		}
	}
	if len(exposed) > 0 {
		cfg.ExposedPorts = exposed
		hc.PortBindings = bindings
	}

	// Volumes / binds
	if len(svc.Volumes) > 0 {
		binds := make([]string, 0, len(svc.Volumes))
		for _, v := range svc.Volumes {
			binds = append(binds, v)
		}
		hc.Binds = binds
		hc.Mounts = nil
	}

	// Environment
	env := map[string]string{}
	if insp.Config != nil {
		for _, e := range insp.Config.Env {
			if i := strings.IndexByte(e, '='); i > 0 {
				env[e[:i]] = e[i+1:]
			}
		}
	}
	for k, v := range svc.Environment {
		env[k] = v
	}
	if len(env) > 0 {
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		cfg.Env = make([]string, 0, len(keys))
		for _, k := range keys {
			cfg.Env = append(cfg.Env, k+"="+env[k])
		}
	}

	// Labels
	if len(svc.Labels) > 0 {
		if cfg.Labels == nil {
			cfg.Labels = map[string]string{}
		}
		for k, v := range svc.Labels {
			cfg.Labels[k] = v
		}
	}

	netCfg := &network.NetworkingConfig{}
	if insp.NetworkSettings != nil && len(insp.NetworkSettings.Networks) > 0 {
		netCfg.EndpointsConfig = insp.NetworkSettings.Networks
	}
	return &cfg, &hc, netCfg, nil
}

// waitUntilHealthy polls the new container for a short window and returns a
// failure reason, or "" when it is running (and healthy if applicable).
func (e *Engine) waitUntilHealthy(ctx context.Context, id string, env map[string]string) string {
	deadline := time.Now().Add(40 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return "context cancelled while waiting for the container"
		case <-time.After(2 * time.Second):
		}
		insp, err := e.docker.ContainerInspect(ctx, id)
		if err != nil {
			return fmt.Sprintf("inspect failed: %v", err)
		}
		if insp.State == nil {
			return "container state unavailable"
		}
		if !insp.State.Running {
			if insp.State.ExitCode != 0 {
				return fmt.Sprintf("container exited with code %d", insp.State.ExitCode)
			}
			return "container is not running"
		}
		if insp.State.Health != nil {
			switch insp.State.Health.Status {
			case "healthy":
				return ""
			case "unhealthy":
				return "container reported unhealthy"
			}
			// starting: keep waiting until the deadline
		} else {
			return ""
		}
		if time.Now().After(deadline) {
			return "container did not become healthy in time"
		}
	}
}

// rollback removes the failed replacement and restores the backup container.
func (e *Engine) rollback(ctx context.Context, failedID, backupID, backupName, originalName string) {
	force := true
	stopTimeout := 10
	_ = e.docker.ContainerStop(ctx, failedID, container.StopOptions{Timeout: &stopTimeout})
	_ = e.docker.ContainerRemove(ctx, failedID, container.RemoveOptions{Force: force})
	e.restoreBackup(backupID, backupName, originalName)
}

// restoreBackup renames the backup container back and starts it again.
func (e *Engine) restoreBackup(backupID, backupName, originalName string) {
	bg := context.Background()
	_ = e.docker.ContainerRename(bg, backupID, originalName)
	_ = e.docker.ContainerStart(bg, backupID, container.StartOptions{})
}
