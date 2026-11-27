package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/overyanosh/homelab-guard/backend/internal/store"
	"github.com/overyanosh/homelab-guard/backend/internal/updater"
)

// StackServiceStatus describes one container of a deployed stack.
type StackServiceStatus struct {
	Service       string `json:"service"`
	ContainerName string `json:"container_name"`
	State         string `json:"state"`
}

// StackInfo is the UI-facing view of a stack.
type StackInfo struct {
	Name      string               `json:"name"`
	Services  []StackServiceStatus `json:"services"`
	Total     int                  `json:"total"`
	Running   int                  `json:"running"`
	UpdatedAt string               `json:"updated_at,omitempty"`
}

// registerStackRoutes wires stack management: deploy a multi-service compose,
// list deployed stacks and tear them down.
func registerStackRoutes(mux *http.ServeMux, dockerCli *client.Client, dbStore *store.Store, engine *updater.Engine) {
	mux.HandleFunc("GET /api/docker/stacks", func(w http.ResponseWriter, r *http.Request) {
		stacks := map[string]*StackInfo{}

		// Live containers labelled with their stack.
		list, err := dockerCli.ContainerList(r.Context(), container.ListOptions{All: true})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		for _, c := range list {
			name := c.Labels["guard.stack"]
			if name == "" {
				continue
			}
			info, ok := stacks[name]
			if !ok {
				info = &StackInfo{Name: name}
				stacks[name] = info
			}
			svc := c.Labels["guard.service"]
			if svc == "" {
				svc = strings.TrimPrefix(c.Names[0], "/")
			}
			state := c.State
			if state == "" {
				state = c.Status
			}
			info.Services = append(info.Services, StackServiceStatus{
				Service:       svc,
				ContainerName: strings.TrimPrefix(c.Names[0], "/"),
				State:         state,
			})
			info.Total++
			if state == "running" {
				info.Running++
			}
		}

		out := make([]StackInfo, 0, len(stacks))
		for _, info := range stacks {
			sort.Slice(info.Services, func(i, j int) bool {
				return info.Services[i].Service < info.Services[j].Service
			})
			out = append(out, *info)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /api/docker/stacks", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
			YAML string `json:"yaml"`
			Env  string `json:"env"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			http.Error(w, "stack name is required", http.StatusBadRequest)
			return
		}
		if result := updater.ValidateCompose(req.YAML, req.Env); !result.Valid {
			writeJSON(w, http.StatusBadRequest, result)
			return
		}

		results, err := engine.DeployStack(r.Context(), name, req.YAML, req.Env)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = dbStore.SaveStack(name, req.YAML, req.Env)
		writeJSON(w, http.StatusOK, map[string]any{"stack": name, "services": results})
	})

	mux.HandleFunc("GET /api/docker/stacks/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		yamlDoc, env, err := dbStore.GetStack(name)
		if err != nil {
			http.Error(w, "stack not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"name": name, "yaml": yamlDoc, "env": env})
	})

	mux.HandleFunc("POST /api/docker/stacks/{name}/down", func(w http.ResponseWriter, r *http.Request) {
		removed, failed := engine.RemoveStack(r.Context(), r.PathValue("name"))
		writeJSON(w, http.StatusOK, map[string]any{"removed": removed, "failed": failed})
	})

	mux.HandleFunc("DELETE /api/docker/stacks/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		removed, failed := engine.RemoveStack(r.Context(), name)
		if len(failed) == 0 {
			_ = dbStore.DeleteStack(name)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"stack":   name,
			"removed": removed,
			"failed":  failed,
		})
	})
}
