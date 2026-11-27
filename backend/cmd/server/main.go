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
	"sort"
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

// containerNetworkNames returns the networks a container is attached to. With
// rootless Podman the Docker-compatible API leaves NetworkSettings.Networks
// empty, so the network mode (pasta, bridge, host, custom network, ...) is used
// as the network identifier instead.
func containerNetworkNames(insp dockertypes.ContainerJSON) []string {
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

// containerPortNames returns human-readable port mappings, e.g. "8080:80/tcp"
// for published ports and "9015/tcp" for merely exposed ones.
func containerPortNames(insp dockertypes.ContainerJSON) []string {
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

// New type for update check response
type UpdateCheckResult struct {
	HasUpdate bool `json:"has_update"`
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// appVersion is surfaced by /api/health and /api/version.
const appVersion = "0.9.5"

var startedAt = time.Now().UTC().Format(time.RFC3339)

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

	runtimeName := detectRuntimeName(dockerCli)
	log.Printf("Container engine detected: %s", runtimeName)

	updateEngine := updater.NewEngine(dockerCli, "http://127.0.0.1:8001")
	hub := agenthub.NewHub()

	mux := http.NewServeMux()

	// Networks, images, volumes and disk-usage management.
	registerResourceRoutes(mux, dockerCli)

	// Multi-service compose stacks.
	registerStackRoutes(mux, dockerCli, dbStore, updateEngine)

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "homelab-guard",
			"version": appVersion,
		})
	})

	// Version/asset stamp: lets the SPA detect it is running an outdated bundle.
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"version":    appVersion,
			"asset":      web.CurrentAsset(),
			"engine":     runtimeName,
			"started_at": startedAt,
		})
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
			networks := []string{}
			ports := []string{}
			if insp, err := dockerCli.ContainerInspect(r.Context(), c.ID); err == nil {
				networks = containerNetworkNames(insp)
				ports = containerPortNames(insp)
			}
			res = append(res, ContainerSummary{
				ID:       c.ID[:12],
				Names:    c.Names,
				Image:    c.Image,
				State:    c.State,
				Status:   c.Status,
				Runtime:  runtimeName,
				Networks: networks,
				Ports:    ports,
				Guard:    labels.ParseDockerLabels(c.Labels),
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

	// --- Container lifecycle actions (start / stop / pause / unpause) ---
	containerAction := func(w http.ResponseWriter, r *http.Request, act func(id string) error, name string) {
		id := r.PathValue("id")
		insp, err := dockerCli.ContainerInspect(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if self, err := os.Hostname(); err == nil && self != "" && strings.HasPrefix(insp.ID, self) {
			http.Error(w, fmt.Sprintf("action « %s » refusée sur le conteneur Homelab Guard lui-même", name), http.StatusConflict)
			return
		}
		if err := act(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}

	mux.HandleFunc("POST /api/docker/containers/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		containerAction(w, r, func(id string) error {
			return dockerCli.ContainerStart(r.Context(), id, container.StartOptions{})
		}, "démarrer")
	})

	mux.HandleFunc("POST /api/docker/containers/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		containerAction(w, r, func(id string) error {
			timeout := 10
			return dockerCli.ContainerStop(r.Context(), id, container.StopOptions{Timeout: &timeout})
		}, "arrêter")
	})

	mux.HandleFunc("POST /api/docker/containers/{id}/pause", func(w http.ResponseWriter, r *http.Request) {
		containerAction(w, r, func(id string) error {
			return dockerCli.ContainerPause(r.Context(), id)
		}, "suspendre")
	})

	mux.HandleFunc("POST /api/docker/containers/{id}/unpause", func(w http.ResponseWriter, r *http.Request) {
		containerAction(w, r, func(id string) error {
			return dockerCli.ContainerUnpause(r.Context(), id)
		}, "reprendre")
	})

	// --- Remove a container (cannot remove Homelab Guard itself) ---
	mux.HandleFunc("DELETE /api/docker/containers/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		insp, err := dockerCli.ContainerInspect(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if self, err := os.Hostname(); err == nil && self != "" && strings.HasPrefix(insp.ID, self) {
			http.Error(w, "impossible de supprimer le conteneur Homelab Guard lui-même", http.StatusConflict)
			return
		}
		force := r.URL.Query().Get("force") == "true"
		if force {
			timeout := 10
			_ = dockerCli.ContainerStop(r.Context(), id, container.StopOptions{Timeout: &timeout})
		}
		if err := dockerCli.ContainerRemove(r.Context(), id, container.RemoveOptions{Force: force}); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// --- Compose validation (used by the non-AI container creation form) ---
	mux.HandleFunc("POST /api/docker/compose/validate", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			YAML string `json:"yaml"`
			Env  string `json:"env"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, updater.ValidateCompose(req.YAML, req.Env))
	})

	// --- Save & apply: recreate the container from the compose definition ---
	mux.HandleFunc("POST /api/docker/containers/compose", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name    string `json:"name"`
			Service string `json:"service"`
			YAML    string `json:"yaml"`
			Env     string `json:"env"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			http.Error(w, "container name is required", http.StatusBadRequest)
			return
		}
		if result := updater.ValidateCompose(req.YAML, req.Env); !result.Valid {
			writeJSON(w, http.StatusBadRequest, result)
			return
		} else if len(result.Services) > 1 {
			http.Error(w,
				"la création simple n'accepte qu'un seul service — utilisez « Stacks » pour déployer plusieurs services",
				http.StatusBadRequest)
			return
		}

		result, err := updateEngine.CreateFromCompose(r.Context(), name, "", req.YAML, req.Env)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Keep the definition for the new container's compose tab.
		service := result.ContainerName
		_ = dbStore.SaveCompose(service, req.YAML)
		_ = dbStore.SaveComposeEnv(service, req.Env)
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /api/docker/containers/{id}/compose/apply", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req struct {
			Service string `json:"service"`
			YAML    string `json:"yaml"`
			Env     string `json:"env"`
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
		service := strings.TrimSpace(req.Service)
		if service == "" {
			service = strings.TrimPrefix(insp.Name, "/")
		}
		if service == "" {
			service = id
		}

		// Validate before touching the running container.
		if result := updater.ValidateCompose(req.YAML, req.Env); !result.Valid {
			writeJSON(w, http.StatusBadRequest, result)
			return
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
	// Live operational data must never be served from the browser cache: it made
	// the "Actualiser" button appear broken and newly created containers invisible.
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// detectRuntimeName identifies the container engine behind the Docker-compatible
// socket. Podman reports a "Podman Engine" component in /version, while Docker
// reports "Engine"/"Docker Engine".
func detectRuntimeName(dockerCli *client.Client) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if v, err := dockerCli.ServerVersion(ctx); err == nil {
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
	if info, err := dockerCli.Info(ctx); err == nil {
		hay := strings.ToLower(info.Name + " " + info.OperatingSystem + " " + info.ServerVersion)
		if strings.Contains(hay, "podman") {
			return "podman"
		}
	}
	return "docker"
}

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
