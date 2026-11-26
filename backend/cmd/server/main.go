package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/gorilla/websocket"
	"github.com/overyanosh/homelab-guard/backend/internal/agenthub"
	"github.com/overyanosh/homelab-guard/backend/internal/crypto"
	"github.com/overyanosh/homelab-guard/backend/internal/labels"
	"github.com/overyanosh/homelab-guard/backend/internal/proxmox"
	sshbastion "github.com/overyanosh/homelab-guard/backend/internal/ssh"
	"github.com/overyanosh/homelab-guard/backend/internal/store"
	"github.com/overyanosh/homelab-guard/backend/internal/updater"
	"github.com/overyanosh/homelab-guard/backend/web"
)

type ContainerSummary struct {
	ID     string             `json:"id"`
	Names  []string           `json:"names"`
	Image  string             `json:"image"`
	State  string             `json:"state"`
	Status string             `json:"status"`
	Guard  labels.GuardConfig `json:"guard"`
}

// New type for update check response
type UpdateCheckResult struct {
	HasUpdate bool `json:"has_update"`
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	vault, err := crypto.NewVault(os.Getenv("MASTER_KEY"))
	if err != nil {
		log.Fatalf("Vault initialization error: %v", err)
	}

	dbStore, err := store.NewStore("/data/homelab-guard.db", vault)
	if err != nil {
		log.Fatalf("SQLite store error: %v", err)
	}

	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Docker client error: %v", err)
	}

	updateEngine := updater.NewEngine(dockerCli, "http://127.0.0.1:8001")
	hub := agenthub.NewHub()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "homelab-guard"})
	})

	// --- Auto-register Local Agent (skip if already exists) ---
	localAgentNode, err := dbStore.CreateAgentNode("Local Host")
	if err != nil {
		// Check if it's just a duplicate name error
		log.Printf("Note: Checking for existing local agent... %v", err)
		// Try to get existing node
		nodes, _ := dbStore.ListAgentNodes()
		for _, n := range nodes {
			if strings.Contains(n.Name, "Local") {
				localAgentNode = &n
				break
			}
		}
		if localAgentNode != nil {
			log.Printf("✅ Found existing local agent (ID: %d, Name: %s)", localAgentNode.ID, localAgentNode.Name)
		} else {
			log.Printf("Warning: Could not register local agent: %v", err)
		}
	} else {
		log.Printf("✅ New local agent auto-registered (ID: %d, Name: %s)", localAgentNode.ID, localAgentNode.Name)
	}

	// --- Remote Agent Hub ---
	mux.HandleFunc("GET /ws/agent", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		node, err := dbStore.ValidateAgentToken(strings.TrimSpace(token))
		if err != nil {
			http.Error(w, "unauthorized agent token", http.StatusUnauthorized)
			return
		}
		ws, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		log.Printf("Agent enrolled & connected: %s (id=%d)", node.Name, node.ID)
		hub.Register(node.ID, node.Name, ws)
	})

	mux.HandleFunc("GET /api/nodes", func(w http.ResponseWriter, r *http.Request) {
		nodes, err := dbStore.ListAgentNodes()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		connected := hub.ListConnectedNodes()

		type nodeStatus struct {
			store.AgentNode
			Online        bool                    `json:"online"`
			LastHeartbeat *agenthub.NodeHeartbeat `json:"last_heartbeat,omitempty"`
		}
		out := make([]nodeStatus, 0, len(nodes))
		for _, n := range nodes {
			item := nodeStatus{AgentNode: n}
			// Mark "Local Host" as always online since it's self-managing
			if strings.Contains(n.Name, "Local") {
				item.Online = true
				hb := agenthub.NodeHeartbeat{
					Hostname:      "localhost",
					SwarmActive:   true,
					SwarmNodeRole: "manager",
					Containers:    0,
					Timestamp:     time.Now().Unix(),
				}
				item.LastHeartbeat = &hb
			} else if live, ok := connected[n.ID]; ok {
				item.Online = true
				hb := live.LastHeartbeat
				item.LastHeartbeat = &hb
			}
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /api/nodes", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			http.Error(w, "node name is required", http.StatusBadRequest)
			return
		}
		node, err := dbStore.CreateAgentNode(strings.TrimSpace(req.Name))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, node)
	})

	mux.HandleFunc("DELETE /api/nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		if err := dbStore.DeleteAgentNode(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /api/nodes/{id}/containers", func(w http.ResponseWriter, r *http.Request) {
		nodeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		raw, err := hub.SendCommand(nodeID, "list_containers", nil, 15*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})

	mux.HandleFunc("POST /api/nodes/{id}/containers/{cid}/upgrade", func(w http.ResponseWriter, r *http.Request) {
		nodeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		payload := map[string]any{
			"container_id": r.PathValue("cid"),
			"force":        r.URL.Query().Get("force") == "true",
		}
		raw, err := hub.SendCommand(nodeID, "upgrade_container", payload, 2*time.Minute)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})

	// --- Local Docker ---
	mux.HandleFunc("GET /api/docker/containers", func(w http.ResponseWriter, r *http.Request) {
		containers, err := dockerCli.ContainerList(context.Background(), container.ListOptions{All: true})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		res := make([]ContainerSummary, 0, len(containers))
		for _, c := range containers {
			res = append(res, ContainerSummary{
				ID:     c.ID[:12],
				Names:  c.Names,
				Image:  c.Image,
				State:  c.State,
				Status: c.Status,
				Guard:  labels.ParseDockerLabels(c.Labels),
			})
		}
		writeJSON(w, http.StatusOK, res)
	})

	// --- Logs Viewer ---
	mux.HandleFunc("GET /api/docker/containers/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		reader, err := dockerCli.ContainerLogs(context.Background(), id, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Tail:       "100",
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		defer reader.Close()
		var buf bytes.Buffer
		io.Copy(&buf, reader)
		w.Header().Set("Content-Type", "text/plain")
		w.Write(buf.Bytes())
	})

	// --- Per-service Compose (view / edit) ---
	mux.HandleFunc("GET /api/docker/containers/{id}/compose", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		insp, err := dockerCli.ContainerInspect(context.Background(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		service := strings.TrimPrefix(insp.Name, "/")
		if service == "" {
			service = id
		}
		yaml, err := dbStore.GetCompose(service)
		source := "stored"
		if err != nil || strings.TrimSpace(yaml) == "" {
			yaml = composeFromInspect(insp)
			source = "generated"
		}
		env, envErr := dbStore.GetComposeEnv(service)
		if envErr != nil || strings.TrimSpace(env) == "" {
			if detected := detectComposeEnv(insp); detected != "" {
				env = detected
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"service":    service,
			"yaml":       yaml,
			"source":     source,
			"env":        env,
			"env_exists": strings.TrimSpace(env) != "",
		})
	})

	mux.HandleFunc("PUT /api/docker/containers/{id}/compose", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req struct {
			YAML string `json:"yaml"`
			Env  string `json:"env"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		insp, err := dockerCli.ContainerInspect(context.Background(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		service := strings.TrimPrefix(insp.Name, "/")
		if service == "" {
			service = id
		}
		if err := dbStore.SaveCompose(service, req.YAML); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := dbStore.SaveComposeEnv(service, req.Env); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"service":    service,
			"yaml":       req.YAML,
			"source":     "stored",
			"env":        req.Env,
			"env_exists": strings.TrimSpace(req.Env) != "",
		})
	})

	// --- Save & apply: recreate the container from the compose definition ---
	mux.HandleFunc("POST /api/docker/containers/{id}/compose/apply", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req struct {
			YAML string `json:"yaml"`
			Env  string `json:"env"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		insp, err := dockerCli.ContainerInspect(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		service := strings.TrimPrefix(insp.Name, "/")
		if service == "" {
			service = id
		}

		// Persist first so the definition is never lost, even if the apply fails.
		if err := dbStore.SaveCompose(service, req.YAML); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := dbStore.SaveComposeEnv(service, req.Env); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		result, err := updateEngine.ApplyCompose(r.Context(), id, service, req.YAML, req.Env)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	// --- Image Version Check ---
	mux.HandleFunc("POST /api/docker/images/check-update", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ImageName string `json:"image_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ImageName == "" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		client := &http.Client{Timeout: 5 * time.Second}
		parts := strings.Split(req.ImageName, "/")
		repo := parts[0]
		tag := "latest"
		if len(parts) > 1 && !strings.Contains(parts[1], ":") {
			repo = parts[1]
		} else if len(parts) > 1 && strings.Contains(parts[1], ":") {
			split := strings.Split(parts[1], ":")
			repo = split[0]
			tag = split[len(split)-1]
		}
		checkURL := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags/%s", repo, tag)
		resp, err := client.Get(checkURL)
		if err != nil || resp.StatusCode != http.StatusOK {
			writeJSON(w, http.StatusOK, map[string]bool{"has_update": false})
			return
		}
		defer resp.Body.Close()
		writeJSON(w, http.StatusOK, map[string]bool{"has_update": true})
	})

	mux.HandleFunc("POST /api/docker/containers/{id}/upgrade", func(w http.ResponseWriter, r *http.Request) {
		force := r.URL.Query().Get("force") == "true"
		result, err := updateEngine.EvaluateAndUpgrade(r.Context(), r.PathValue("id"), force)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /api/docker/fleet/reconcile", func(w http.ResponseWriter, r *http.Request) {
		containers, err := dockerCli.ContainerList(r.Context(), container.ListOptions{All: false})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		results := make([]*updater.UpdateResult, 0)
		for _, c := range containers {
			cfg := labels.ParseDockerLabels(c.Labels)
			if !cfg.Enabled {
				continue
			}
			res, err := updateEngine.EvaluateAndUpgrade(r.Context(), c.ID, false)
			if err != nil {
				results = append(results, &updater.UpdateResult{
					ContainerName: c.Names[0],
					Image:         c.Image,
					Status:        "error: " + err.Error(),
				})
				continue
			}
			results = append(results, res)
		}
		writeJSON(w, http.StatusOK, results)
	})

	// --- SSH Bastion ---
	mux.HandleFunc("GET /api/ssh/hosts", func(w http.ResponseWriter, r *http.Request) {
		hosts, err := dbStore.ListSSHHosts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, hosts)
	})

	mux.HandleFunc("POST /api/ssh/hosts", func(w http.ResponseWriter, r *http.Request) {
		var req store.SSHHost
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		id, pubKey, err := dbStore.SaveSSHHost(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.ID = id
		req.PrivateKeyPEM = ""
		req.PublicKey = pubKey
		writeJSON(w, http.StatusCreated, req)
	})

	mux.HandleFunc("DELETE /api/ssh/hosts/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid host id", http.StatusBadRequest)
			return
		}
		if err := dbStore.DeleteSSHHost(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /ws/ssh/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid host id", http.StatusBadRequest)
			return
		}
		host, err := dbStore.GetSSHHost(id)
		if err != nil {
			http.Error(w, "ssh host not found", http.StatusNotFound)
			return
		}
		ws, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		_ = sshbastion.BridgeWebSocketToSSH(ws, sshbastion.SessionRequest{
			Host:          host.Host,
			Port:          host.Port,
			User:          host.Username,
			PrivateKeyPEM: []byte(host.PrivateKeyPEM),
		})
	})

	// --- AI Agent Proxy ---
	mux.HandleFunc("POST /api/ai/generate-compose", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		resp, err := http.Post("http://127.0.0.1:8001/ai/generate-compose", "application/json", bytes.NewReader(body))
		if err != nil {
			http.Error(w, fmt.Sprintf("ai-worker error: %v", err), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})

	mux.HandleFunc("GET /api/ai/config", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{
			"worker_online": false,
			"model":         "",
			"base_url":      "",
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://127.0.0.1:8001/health")
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var h struct {
					Status  string `json:"status"`
					Model   string `json:"model"`
					BaseURL string `json:"base_url"`
				}
				if json.NewDecoder(resp.Body).Decode(&h) == nil {
					out["worker_online"] = true
					out["model"] = h.Model
					out["base_url"] = h.BaseURL
				}
			}
		}
		writeJSON(w, http.StatusOK, out)
	})

	// --- Proxmox VE ---
	mux.HandleFunc("GET /api/proxmox/clusters", func(w http.ResponseWriter, r *http.Request) {
		clusters, err := dbStore.ListProxmoxClusters()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, clusters)
	})

	mux.HandleFunc("POST /api/proxmox/clusters", func(w http.ResponseWriter, r *http.Request) {
		var req store.ProxmoxCluster
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		pve := proxmox.NewClient(req.Endpoint, req.TokenID, req.TokenSecret, req.InsecureTLS)
		if _, err := pve.GetClusterResources("node"); err != nil {
			http.Error(w, fmt.Sprintf("proxmox connection failed: %v", err), http.StatusBadGateway)
			return
		}
		id, err := dbStore.SaveProxmoxCluster(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.ID = id
		req.TokenSecret = ""
		writeJSON(w, http.StatusCreated, req)
	})

	mux.HandleFunc("DELETE /api/proxmox/clusters/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid cluster id", http.StatusBadRequest)
			return
		}
		if err := dbStore.DeleteProxmoxCluster(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /api/proxmox/clusters/{id}/resources", func(w http.ResponseWriter, r *http.Request) {
		pve, err := getPVEClient(dbStore, r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resources, err := pve.GetClusterResources(r.URL.Query().Get("type"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, resources)
	})

	mux.HandleFunc("POST /api/proxmox/clusters/{id}/nodes/{node}/{type}/{vmid}/power/{action}", func(w http.ResponseWriter, r *http.Request) {
		pve, err := getPVEClient(dbStore, r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		vmid, err := strconv.Atoi(r.PathValue("vmid"))
		if err != nil {
			http.Error(w, "invalid vmid", http.StatusBadRequest)
			return
		}
		upid, err := pve.GuestPowerAction(r.PathValue("node"), r.PathValue("type"), vmid, r.PathValue("action"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"task_upid": upid})
	})

	mux.HandleFunc("POST /api/proxmox/clusters/{id}/nodes/{node}/{type}/{vmid}/snapshot", func(w http.ResponseWriter, r *http.Request) {
		pve, err := getPVEClient(dbStore, r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		vmid, _ := strconv.Atoi(r.PathValue("vmid"))
		snapName := fmt.Sprintf("guard_%d", time.Now().Unix())
		upid, err := pve.CreateSnapshot(r.PathValue("node"), r.PathValue("type"), vmid, snapName, "Homelab Guard automated snapshot", false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"snapshot": snapName, "task_upid": upid})
	})

	mux.Handle("/", web.SPAHandler())

	addr := ":9015"
	log.Printf("Homelab Guard Core listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func getPVEClient(s *store.Store, rawID string) (*proxmox.Client, error) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid cluster id")
	}
	cluster, err := s.GetProxmoxCluster(id)
	if err != nil {
		return nil, err
	}
	return proxmox.NewClient(cluster.Endpoint, cluster.TokenID, cluster.TokenSecret, cluster.InsecureTLS), nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
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
