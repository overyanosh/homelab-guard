package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/gorilla/websocket"
	"github.com/overyanosh/homelab-guard/backend/internal/composegen"
	"github.com/overyanosh/homelab-guard/backend/internal/ctrinfo"
	"github.com/overyanosh/homelab-guard/backend/internal/dockerops"
	"github.com/overyanosh/homelab-guard/backend/internal/labels"
	"github.com/overyanosh/homelab-guard/backend/internal/updater"
)

// agentVersion is logged at startup so the deployed build can be identified.
const agentVersion = "0.9.22"

type AgentMessage struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Action    string          `json:"action,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type NodeHeartbeat struct {
	Hostname      string `json:"hostname"`
	SwarmActive   bool   `json:"swarm_active"`
	SwarmNodeRole string `json:"swarm_node_role,omitempty"`
	Containers    int    `json:"containers"`
	Timestamp     int64  `json:"timestamp"`
}

func main() {
	serverURL := os.Getenv("GUARD_SERVER_URL")
	enrollToken := os.Getenv("GUARD_ENROLL_TOKEN")
	if serverURL == "" || enrollToken == "" {
		log.Fatal("GUARD_SERVER_URL and GUARD_ENROLL_TOKEN are required")
	}

	wsURL := normalizeServerURL(serverURL)
	log.Printf("Homelab Guard agent %s starting (server=%q, websocket=%q)", agentVersion, serverURL, wsURL)

	host, tried := resolveDockerHost()
	if host == "" {
		log.Printf("Warning: no Docker socket found (tried: %s). Set DOCKER_HOST or mount the socket.",
			strings.Join(tried, ", "))
		host = "unix:///var/run/docker.sock"
	}
	log.Printf("Docker endpoint: %s", host)

	dockerCli, err := client.NewClientWithOpts(
		client.WithHost(host),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		log.Fatalf("Docker client error: %v", err)
	}

	// Probe the API once: the Docker client reports any dial failure (including
	// EACCES on a mounted socket) as "Cannot connect ... Is the docker daemon
	// running?", which is misleading. Log an actionable warning instead.
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if _, err := dockerCli.Ping(pingCtx); err != nil {
		log.Printf("Warning: Docker API unreachable at %s: %v", host, err)
		log.Printf("  → soit le socket n'est pas monté (-v /var/run/docker.sock:/var/run/docker.sock),")
		log.Printf("  → soit l'utilisateur de l'agent n'y a pas accès : lancer avec --group-add <gid du socket> (ici 988)")
		log.Printf("    ou --user 0:0. Vérifier sur l'hôte : stat -c %%g /var/run/docker.sock")
	} else {
		log.Printf("Docker API reachable (%s)", host)
	}
	pingCancel()

	engine := updater.NewEngine(dockerCli, serverURL)

	tlsCfg, err := buildMTLSConfig()
	if err != nil {
		log.Fatalf("mTLS configuration error: %v", err)
	}

	dialer := websocket.Dialer{
		TLSClientConfig:  tlsCfg,
		HandshakeTimeout: 15 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down homelab-guard-agent")
			return
		default:
			if err := runSession(ctx, dialer, wsURL, enrollToken, dockerCli, engine); err != nil {
				log.Printf("Connection lost (%v), reconnecting in 5s...", err)
				time.Sleep(5 * time.Second)
			}
		}
	}
}

func buildMTLSConfig() (*tls.Config, error) {
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: os.Getenv("GUARD_INSECURE_TLS") == "true",
	}

	certFile := os.Getenv("GUARD_CLIENT_CERT")
	keyFile := os.Getenv("GUARD_CLIENT_KEY")
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	if caFile := os.Getenv("GUARD_CA_CERT"); caFile != "" {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caPEM)
		tlsCfg.RootCAs = pool
	}
	return tlsCfg, nil
}

// isSelfContainer reports whether id is the agent's own container.
func isSelfContainer(id string) bool {
	h, err := os.Hostname()
	return err == nil && h != "" && strings.HasPrefix(id, h)
}

// resolveDockerHost returns the first reachable Docker/Podman socket. DOCKER_HOST
// always wins; otherwise the usual docker and podman locations are probed.
func resolveDockerHost() (string, []string) {
	if h := strings.TrimSpace(os.Getenv("DOCKER_HOST")); h != "" {
		return h, nil
	}
	xdg := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	uid := os.Getuid()
	candidates := []string{
		"unix:///var/run/docker.sock",
		"unix:///run/docker.sock",
	}
	if xdg != "" {
		candidates = append(candidates, "unix://"+filepath.Join(xdg, "docker.sock"))
	}
	candidates = append(candidates,
		fmt.Sprintf("unix:///run/user/%d/docker.sock", uid),
		"unix:///run/podman/podman.sock",
	)
	if xdg != "" {
		candidates = append(candidates, "unix://"+filepath.Join(xdg, "podman", "podman.sock"))
	}
	candidates = append(candidates, fmt.Sprintf("unix:///run/user/%d/podman/podman.sock", uid))

	probed := make([]string, 0, len(candidates))
	for _, c := range candidates {
		path := strings.TrimPrefix(c, "unix://")
		probed = append(probed, path)
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return c, probed
		}
	}
	return "", probed
}

// dockerErrorHint makes socket problems actionable in the UI.
func dockerErrorHint(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "Cannot connect to the Docker daemon") ||
		strings.Contains(msg, "no such file or directory") && strings.Contains(msg, ".sock") {
		return msg + " — deux causes possibles : (1) le socket n'est pas monté " +
			"(-v /var/run/docker.sock:/var/run/docker.sock) ; (2) l'utilisateur de l'agent " +
			"n'a pas les droits dessus (socket root:docker 660) : relancer avec " +
			"--group-add <gid du socket> ou --user 0:0."
	}
	if strings.Contains(msg, "permission denied") {
		return msg + " — droits insuffisants sur le socket : ajoutez le groupe docker " +
			"(--group-add <gid du socket>) ou exécutez avec --user 0:0."
	}
	return msg
}

// normalizeServerURL accepts http(s)://, ws(s):// or a bare host and returns a
// websocket base URL. The UI generates http(s) URLs, so this keeps both forms
// working instead of failing with "malformed ws or wss URL".
func normalizeServerURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = strings.TrimSuffix(u, "/")
	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://")
	case strings.HasPrefix(u, "wss://"), strings.HasPrefix(u, "ws://"):
		return u
	default:
		return "ws://" + u
	}
}

func runSession(ctx context.Context, dialer websocket.Dialer, serverURL, token string, dockerCli *client.Client, engine *updater.Engine) error {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)

	ws, _, err := dialer.DialContext(ctx, serverURL+"/ws/agent", headers)
	if err != nil {
		return err
	}
	defer ws.Close()
	log.Printf("Connected to control plane: %s", serverURL)

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hb := collectHeartbeat(ctx, dockerCli)
				raw, _ := json.Marshal(hb)
				_ = ws.WriteJSON(AgentMessage{Type: "heartbeat", Payload: raw})
			}
		}
	}()

	for {
		var msg AgentMessage
		if err := ws.ReadJSON(&msg); err != nil {
			return err
		}
		if msg.Type != "command" {
			continue
		}
		go handleCommand(ctx, ws, dockerCli, engine, msg)
	}
}

func collectHeartbeat(ctx context.Context, dockerCli *client.Client) NodeHeartbeat {
	hostname, _ := os.Hostname()
	info, _ := dockerCli.Info(ctx)
	containers, _ := dockerCli.ContainerList(ctx, container.ListOptions{All: true})

	role := ""
	if info.Swarm.LocalNodeState == "active" {
		role = "worker"
		if info.Swarm.ControlAvailable {
			role = "manager"
		}
	}

	return NodeHeartbeat{
		Hostname:      hostname,
		SwarmActive:   info.Swarm.LocalNodeState == "active",
		SwarmNodeRole: role,
		Containers:    len(containers),
		Timestamp:     time.Now().Unix(),
	}
}

func handleCommand(ctx context.Context, ws *websocket.Conn, dockerCli *client.Client, engine *updater.Engine, msg AgentMessage) {
	var respPayload any

	switch msg.Action {
	case "list_containers":
		list, err := dockerCli.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			respPayload = map[string]string{"error": dockerErrorHint(err)}
			break
		}
		type summary struct {
			ID       string             `json:"id"`
			Names    []string           `json:"names"`
			Image    string             `json:"image"`
			State    string             `json:"state"`
			Status   string             `json:"status"`
			Runtime  string             `json:"runtime"`
			Networks []string           `json:"networks"`
			Ports    []string           `json:"ports"`
			Guard    labels.GuardConfig `json:"guard"`
		}
		runtimeName := ctrinfo.RuntimeName(dockerCli)
		out := make([]summary, 0, len(list))
		for _, c := range list {
			item := summary{
				ID:      ctrinfo.ShortID(c.ID),
				Names:   c.Names,
				Image:   c.Image,
				State:   c.State,
				Status:  c.Status,
				Runtime: runtimeName,
				Guard:   labels.ParseDockerLabels(c.Labels),
			}
			if insp, err := dockerCli.ContainerInspect(ctx, c.ID); err == nil {
				item.Networks = ctrinfo.NetworkNames(insp)
				item.Ports = ctrinfo.PortNames(insp)
			}
			out = append(out, item)
		}
		respPayload = out

	case "upgrade_container":
		var req struct {
			ContainerID string `json:"container_id"`
			Force       bool   `json:"force"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		res, err := engine.EvaluateAndUpgrade(ctx, req.ContainerID, req.Force)
		if err != nil {
			respPayload = map[string]string{"error": err.Error()}
		} else {
			respPayload = res
		}

	case "container_action":
		var req struct {
			ContainerID string `json:"container_id"`
			Action      string `json:"action"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		if isSelfContainer(req.ContainerID) {
			respPayload = map[string]string{"error": "action refusée sur le conteneur de l'agent lui-même"}
			break
		}
		var err error
		timeout := 10
		switch req.Action {
		case "start":
			err = dockerCli.ContainerStart(ctx, req.ContainerID, container.StartOptions{})
		case "stop":
			err = dockerCli.ContainerStop(ctx, req.ContainerID, container.StopOptions{Timeout: &timeout})
		case "restart":
			err = dockerCli.ContainerRestart(ctx, req.ContainerID, container.StopOptions{Timeout: &timeout})
		case "pause":
			err = dockerCli.ContainerPause(ctx, req.ContainerID)
		case "unpause":
			err = dockerCli.ContainerUnpause(ctx, req.ContainerID)
		default:
			err = fmt.Errorf("action inconnue: %s", req.Action)
		}
		if err != nil {
			respPayload = map[string]string{"error": dockerErrorHint(err)}
		} else {
			respPayload = map[string]string{"status": "ok", "action": req.Action}
		}

	case "container_logs":
		var req struct {
			ContainerID string `json:"container_id"`
			Tail        string `json:"tail"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		tail := req.Tail
		if tail == "" {
			tail = "200"
		}
		reader, err := dockerCli.ContainerLogs(ctx, req.ContainerID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Tail:       tail,
		})
		if err != nil {
			respPayload = map[string]string{"error": dockerErrorHint(err)}
			break
		}
		var buf bytes.Buffer
		_, _ = stdcopy.StdCopy(&buf, &buf, reader)
		_ = reader.Close()
		respPayload = map[string]string{"logs": buf.String()}

	case "container_remove":
		var req struct {
			ContainerID string `json:"container_id"`
			Force       bool   `json:"force"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		if isSelfContainer(req.ContainerID) {
			respPayload = map[string]string{"error": "suppression refusée sur le conteneur de l'agent lui-même"}
			break
		}
		if req.Force {
			timeout := 10
			_ = dockerCli.ContainerStop(ctx, req.ContainerID, container.StopOptions{Timeout: &timeout})
		}
		if err := dockerCli.ContainerRemove(ctx, req.ContainerID, container.RemoveOptions{Force: req.Force}); err != nil {
			respPayload = map[string]string{"error": dockerErrorHint(err)}
		} else {
			respPayload = map[string]string{"status": "ok"}
		}

	case "networks_list":
		out, err := dockerops.Networks(ctx, dockerCli)
		if err != nil {
			respPayload = map[string]string{"error": dockerErrorHint(err)}
		} else {
			respPayload = out
		}

	case "network_create":
		var req dockerops.NetworkCreateRequest
		_ = json.Unmarshal(msg.Payload, &req)
		id, err := dockerops.CreateNetwork(ctx, dockerCli, req)
		if err != nil {
			respPayload = map[string]string{"error": dockerErrorHint(err)}
		} else {
			respPayload = map[string]string{"id": id, "name": req.Name}
		}

	case "network_remove":
		var req struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		if err := dockerops.RemoveNetwork(ctx, dockerCli, req.ID); err != nil {
			respPayload = map[string]string{"error": dockerErrorHint(err)}
		} else {
			respPayload = map[string]string{"status": "ok"}
		}

	case "networks_prune":
		deleted, err := dockerops.PruneNetworks(ctx, dockerCli)
		if err != nil {
			respPayload = map[string]string{"error": dockerErrorHint(err)}
		} else {
			respPayload = map[string]any{"deleted": deleted}
		}

	case "images_list":
		respPayload = dockerops.Images(ctx, dockerCli)

	case "image_remove":
		var req struct {
			ID    string `json:"id"`
			Force bool   `json:"force"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		if err := dockerops.RemoveImage(ctx, dockerCli, req.ID, req.Force); err != nil {
			respPayload = map[string]string{"error": err.Error()}
		} else {
			respPayload = map[string]string{"status": "ok"}
		}

	case "images_prune":
		deleted, reclaimed := dockerops.PruneImages(ctx, dockerCli)
		respPayload = map[string]any{"deleted": deleted, "space_reclaimed": reclaimed}

	case "volumes_list":
		respPayload = dockerops.Volumes(ctx, dockerCli)

	case "volume_create":
		var req struct {
			Name   string `json:"name"`
			Driver string `json:"driver"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		if err := dockerops.CreateVolume(ctx, dockerCli, req.Name, req.Driver); err != nil {
			respPayload = map[string]string{"error": dockerErrorHint(err)}
		} else {
			respPayload = map[string]string{"name": req.Name}
		}

	case "volume_remove":
		var req struct {
			Name  string `json:"name"`
			Force bool   `json:"force"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		if err := dockerops.RemoveVolume(ctx, dockerCli, req.Name, req.Force); err != nil {
			respPayload = map[string]string{"error": dockerErrorHint(err)}
		} else {
			respPayload = map[string]string{"status": "ok"}
		}

	case "volumes_prune":
		deleted, reclaimed := dockerops.PruneVolumes(ctx, dockerCli)
		respPayload = map[string]any{"deleted": deleted, "space_reclaimed": reclaimed}

	case "disk_usage":
		respPayload = dockerops.DiskUsage(ctx, dockerCli)

	case "build_cache_prune":
		report, err := dockerCli.BuildCachePrune(ctx, dockerTypes.BuildCachePruneOptions{All: true})
		if err != nil {
			respPayload = map[string]any{"supported": false, "space_reclaimed": 0, "message": err.Error()}
		} else {
			respPayload = map[string]any{"supported": true, "space_reclaimed": report.SpaceReclaimed}
		}

	case "compose_get":
		var req struct {
			ContainerID string `json:"container_id"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		insp, err := dockerCli.ContainerInspect(ctx, req.ContainerID)
		if err != nil {
			respPayload = map[string]string{"error": err.Error()}
			break
		}
		service := strings.TrimPrefix(insp.Name, "/")
		generated := composegen.FromInspect(insp)
		env := composegen.DetectEnvFile(insp)
		respPayload = map[string]any{
			"service":    service,
			"yaml":       generated,
			"source":     "generated",
			"env":        env,
			"env_exists": strings.TrimSpace(env) != "",
		}

	case "compose_apply":
		var req struct {
			ContainerID string `json:"container_id"`
			Service     string `json:"service"`
			YAML        string `json:"yaml"`
			Env         string `json:"env"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		res, err := engine.ApplyCompose(ctx, req.ContainerID, req.Service, req.YAML, req.Env)
		if err != nil {
			respPayload = map[string]string{"error": err.Error()}
		} else {
			respPayload = res
		}

	case "container_create":
		var req struct {
			Name    string `json:"name"`
			Service string `json:"service"`
			YAML    string `json:"yaml"`
			Env     string `json:"env"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		res, err := engine.CreateFromCompose(ctx, req.Name, req.Service, req.YAML, req.Env)
		if err != nil {
			respPayload = map[string]string{"error": err.Error()}
		} else {
			respPayload = res
		}

	case "stacks_list":
		list, err := dockerCli.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			respPayload = map[string]string{"error": dockerErrorHint(err)}
			break
		}
		type svc struct {
			Service       string `json:"service"`
			ContainerName string `json:"container_name"`
			State         string `json:"state"`
		}
		type stack struct {
			Name     string `json:"name"`
			Services []svc  `json:"services"`
			Total    int    `json:"total"`
			Running  int    `json:"running"`
		}
		byName := map[string]*stack{}
		for _, c := range list {
			name := strings.TrimSpace(c.Labels["guard.stack"])
			if name == "" {
				continue
			}
			st, ok := byName[name]
			if !ok {
				st = &stack{Name: name}
				byName[name] = st
			}
			service := c.Labels["guard.service"]
			if service == "" && len(c.Names) > 0 {
				service = strings.TrimPrefix(c.Names[0], "/")
			}
			containerName := ""
			if len(c.Names) > 0 {
				containerName = strings.TrimPrefix(c.Names[0], "/")
			}
			st.Services = append(st.Services, svc{Service: service, ContainerName: containerName, State: c.State})
			st.Total++
			if c.State == "running" {
				st.Running++
			}
		}
		out := make([]*stack, 0, len(byName))
		for _, st := range byName {
			out = append(out, st)
		}
		respPayload = out

	case "stack_deploy":
		var req struct {
			Name string `json:"name"`
			YAML string `json:"yaml"`
			Env  string `json:"env"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		results, err := engine.DeployStack(ctx, req.Name, req.YAML, req.Env)
		if err != nil {
			respPayload = map[string]string{"error": err.Error()}
		} else {
			respPayload = map[string]any{"stack": req.Name, "services": results}
		}

	case "stack_remove":
		var req struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(msg.Payload, &req)
		removed, failed := engine.RemoveStack(ctx, req.Name)
		respPayload = map[string]any{"stack": req.Name, "removed": removed, "failed": failed}
	}

	raw, _ := json.Marshal(respPayload)
	_ = ws.WriteJSON(AgentMessage{
		Type:      "response",
		RequestID: msg.RequestID,
		Payload:   raw,
	})
}
