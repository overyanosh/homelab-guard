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
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/gorilla/websocket"
	"homelab-guard/internal/agenthub"
	"homelab-guard/internal/crypto"
	"homelab-guard/internal/labels"
	"homelab-guard/internal/proxmox"
	sshbastion "homelab-guard/internal/ssh"
	"homelab-guard/internal/store"
	"homelab-guard/internal/updater"
	"homelab-guard/web"
)

type ContainerSummary struct {
	ID     string             `json:"id"`
	Names  []string           `json:"names"`
	Image  string             `json:"image"`
	State  string             `json:"state"`
	Status string             `json:"status"`
	Guard  labels.GuardConfig `json:"guard"`
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
			if live, ok := connected[n.ID]; ok {
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

	addr := ":8443"
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
