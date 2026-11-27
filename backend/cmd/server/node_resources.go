package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/overyanosh/homelab-guard/backend/internal/agenthub"
)

// registerNodeResourceRoutes exposes the same Docker operations as the local
// engine, but executed by a remote agent: /api/nodes/{id}/networks, /images,
// /volumes, /disk-usage.
func registerNodeResourceRoutes(mux *http.ServeMux, hub *agenthub.Hub) {
	// Generic proxy: send a command to the agent and stream back its payload.
	proxy := func(action string, timeout time.Duration, pathKey, payloadName string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			nodeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if err != nil {
				http.Error(w, "invalid node id", http.StatusBadRequest)
				return
			}
			payload := map[string]any{}
			if pathKey != "" {
				payload[payloadName] = r.PathValue(pathKey)
			}
			for k, v := range r.URL.Query() {
				if len(v) > 0 {
					payload[k] = v[0]
				}
			}
			if r.Body != nil && r.ContentLength != 0 {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
					for k, v := range body {
						payload[k] = v
					}
				}
			}

			raw, err := hub.SendCommand(nodeID, action, payload, timeout)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			if msg := commandError(raw); msg != "" {
				http.Error(w, msg, http.StatusBadGateway)
				return
			}
			if len(raw) == 0 {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store, max-age=0")
			_, _ = w.Write(raw)
		}
	}

	mux.HandleFunc("GET /api/nodes/{id}/networks", proxy("networks_list", 20*time.Second, "", ""))
	mux.HandleFunc("POST /api/nodes/{id}/networks", proxy("network_create", 30*time.Second, "", ""))
	mux.HandleFunc("DELETE /api/nodes/{id}/networks/{nid}", proxy("network_remove", 30*time.Second, "nid", "id"))
	mux.HandleFunc("POST /api/nodes/{id}/networks/prune", proxy("networks_prune", 60*time.Second, "", ""))

	mux.HandleFunc("GET /api/nodes/{id}/images", proxy("images_list", 30*time.Second, "", ""))
	mux.HandleFunc("DELETE /api/nodes/{id}/images/{iid}", proxy("image_remove", 120*time.Second, "iid", "id"))
	mux.HandleFunc("POST /api/nodes/{id}/images/prune", proxy("images_prune", 300*time.Second, "", ""))

	mux.HandleFunc("GET /api/nodes/{id}/volumes", proxy("volumes_list", 30*time.Second, "", ""))
	mux.HandleFunc("POST /api/nodes/{id}/volumes", proxy("volume_create", 30*time.Second, "", ""))
	mux.HandleFunc("DELETE /api/nodes/{id}/volumes/{name}", proxy("volume_remove", 60*time.Second, "name", "name"))
	mux.HandleFunc("POST /api/nodes/{id}/volumes/prune", proxy("volumes_prune", 120*time.Second, "", ""))

	mux.HandleFunc("GET /api/nodes/{id}/disk-usage", proxy("disk_usage", 30*time.Second, "", ""))
	mux.HandleFunc("POST /api/nodes/{id}/build-cache/prune", proxy("build_cache_prune", 120*time.Second, "", ""))
}
