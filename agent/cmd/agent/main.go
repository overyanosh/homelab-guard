package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"log"
	"net/http"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/gorilla/websocket"
	"github.com/overyanosh/homelab-guard/backend/internal/ctrinfo"
	"github.com/overyanosh/homelab-guard/backend/internal/labels"
	"github.com/overyanosh/homelab-guard/backend/internal/updater"
)

// agentVersion is logged at startup so the deployed build can be identified.
const agentVersion = "0.9.20"

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
	}

	raw, _ := json.Marshal(respPayload)
	_ = ws.WriteJSON(AgentMessage{
		Type:      "response",
		RequestID: msg.RequestID,
		Payload:   raw,
	})
}
