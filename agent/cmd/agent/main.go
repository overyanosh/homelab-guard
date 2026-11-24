package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/gorilla/websocket"
	"homelab-guard/internal/labels"
	"homelab-guard/internal/updater"
)

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

	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Docker client error: %v", err)
	}

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
			if err := runSession(ctx, dialer, serverURL, enrollToken, dockerCli, engine); err != nil {
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
			respPayload = map[string]string{"error": err.Error()}
			break
		}
		type summary struct {
			ID     string             `json:"id"`
			Names  []string           `json:"names"`
			Image  string             `json:"image"`
			State  string             `json:"state"`
			Status string             `json:"status"`
			Guard  labels.GuardConfig `json:"guard"`
		}
		out := make([]summary, 0, len(list))
		for _, c := range list {
			out = append(out, summary{
				ID:     c.ID[:12],
				Names:  c.Names,
				Image:  c.Image,
				State:  c.State,
				Status: c.Status,
				Guard:  labels.ParseDockerLabels(c.Labels),
			})
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
