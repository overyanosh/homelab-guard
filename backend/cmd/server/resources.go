package main

import (
	"encoding/json"
	"net/http"
	"strings"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/overyanosh/homelab-guard/backend/internal/dockerops"
)

// registerResourceRoutes exposes networks, images, volumes and disk usage for
// the LOCAL engine.
func registerResourceRoutes(mux *http.ServeMux, dockerCli *client.Client) {
	mux.HandleFunc("GET /api/docker/networks", func(w http.ResponseWriter, r *http.Request) {
		out, err := dockerops.Networks(r.Context(), dockerCli)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /api/docker/networks", func(w http.ResponseWriter, r *http.Request) {
		var req dockerops.NetworkCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			http.Error(w, "network name is required", http.StatusBadRequest)
			return
		}
		id, err := dockerops.CreateNetwork(r.Context(), dockerCli, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "name": req.Name})
	})

	mux.HandleFunc("DELETE /api/docker/networks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := dockerops.RemoveNetwork(r.Context(), dockerCli, r.PathValue("id")); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /api/docker/networks/prune", func(w http.ResponseWriter, r *http.Request) {
		deleted, err := dockerops.PruneNetworks(r.Context(), dockerCli)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
	})

	mux.HandleFunc("GET /api/docker/images", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, dockerops.Images(r.Context(), dockerCli))
	})

	mux.HandleFunc("DELETE /api/docker/images/{id}", func(w http.ResponseWriter, r *http.Request) {
		force := r.URL.Query().Get("force") == "true"
		if err := dockerops.RemoveImage(r.Context(), dockerCli, r.PathValue("id"), force); err != nil {
			status := http.StatusBadGateway
			if strings.Contains(err.Error(), "used by a running") {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /api/docker/images/prune", func(w http.ResponseWriter, r *http.Request) {
		deleted, reclaimed := dockerops.PruneImages(r.Context(), dockerCli)
		writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "space_reclaimed": reclaimed})
	})

	mux.HandleFunc("GET /api/docker/volumes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, dockerops.Volumes(r.Context(), dockerCli))
	})

	mux.HandleFunc("POST /api/docker/volumes", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name   string `json:"name"`
			Driver string `json:"driver"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			http.Error(w, "volume name is required", http.StatusBadRequest)
			return
		}
		if err := dockerops.CreateVolume(r.Context(), dockerCli, req.Name, req.Driver); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name})
	})

	mux.HandleFunc("DELETE /api/docker/volumes/{name}", func(w http.ResponseWriter, r *http.Request) {
		force := r.URL.Query().Get("force") == "true"
		if err := dockerops.RemoveVolume(r.Context(), dockerCli, r.PathValue("name"), force); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /api/docker/volumes/prune", func(w http.ResponseWriter, r *http.Request) {
		deleted, reclaimed := dockerops.PruneVolumes(r.Context(), dockerCli)
		writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "space_reclaimed": reclaimed})
	})

	mux.HandleFunc("GET /api/docker/disk-usage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, dockerops.DiskUsage(r.Context(), dockerCli))
	})

	mux.HandleFunc("POST /api/docker/build-cache/prune", func(w http.ResponseWriter, r *http.Request) {
		report, err := dockerCli.BuildCachePrune(r.Context(), dockerTypes.BuildCachePruneOptions{All: true})
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				writeJSON(w, http.StatusOK, map[string]any{
					"supported":       false,
					"space_reclaimed": 0,
					"message":         "Le cache de build n'est pas géré par ce moteur (Podman).",
				})
				return
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"supported":       true,
			"space_reclaimed": report.SpaceReclaimed,
		})
	})
}
