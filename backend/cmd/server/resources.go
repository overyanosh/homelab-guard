package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// NetworkInfo is the UI-facing view of a container network.
type NetworkInfo struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Driver         string            `json:"driver"`
	Scope          string            `json:"scope"`
	Internal       bool              `json:"internal"`
	Attachable     bool              `json:"attachable"`
	Created        string            `json:"created"`
	Containers     int               `json:"containers"`
	InUse          bool              `json:"in_use"`
	ContainerNames []string          `json:"container_names,omitempty"`
	// Virtual entries represent network modes (pasta, host, none, custom mode)
	// that are used by containers but have no network object to manage.
	Virtual bool              `json:"virtual,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

// ImageInfo is the UI-facing view of a local image.
type ImageInfo struct {
	ID         string   `json:"id"`
	ShortID    string   `json:"short_id"`
	Tags       []string `json:"tags"`
	Size       int64    `json:"size"`
	Created    int64    `json:"created"`
	Dangling   bool     `json:"dangling"`
	InUse      bool     `json:"in_use"`
	Containers int64    `json:"containers"`
}

// VolumeInfo is the UI-facing view of a volume (container scratch data).
type VolumeInfo struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
	CreatedAt  string `json:"created_at"`
	Size       int64  `json:"size"`
	RefCount   int64  `json:"ref_count"`
	InUse      bool   `json:"in_use"`
	Scope      string `json:"scope"`
}

// DiskUsageInfo summarises space used and reclaimable.
type DiskUsageInfo struct {
	ImagesSize      int64 `json:"images_size"`
	ImagesReclaim   int64 `json:"images_reclaim"`
	ContainersSize  int64 `json:"containers_size"`
	ContainersCount int   `json:"containers_count"`
	VolumesSize     int64 `json:"volumes_size"`
	VolumesReclaim  int64 `json:"volumes_reclaim"`
	BuildCacheSize  int64 `json:"build_cache_size"`
	BuildCacheCount int   `json:"build_cache_count"`
	TotalReclaim    int64 `json:"total_reclaim"`
}

func registerResourceRoutes(mux *http.ServeMux, dockerCli *client.Client) {
	// ---------------- Networks ----------------

	mux.HandleFunc("GET /api/docker/networks", func(w http.ResponseWriter, r *http.Request) {
		nets, err := dockerCli.NetworkList(r.Context(), network.ListOptions{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		// NetworkList.Containers is unreliable (empty with Podman, not always
		// populated by Docker), so usage is derived from container inspections —
		// the same source as the containers table.
		usage := networkUsage(r.Context(), dockerCli)

		out := make([]NetworkInfo, 0, len(nets))
		known := map[string]bool{}
		for _, n := range nets {
			known[n.Name] = true
			names := usage[n.Name]
			count := len(names)
			if len(n.Containers) > count {
				count = len(n.Containers)
			}
			out = append(out, NetworkInfo{
				ID:             n.ID,
				Name:           n.Name,
				Driver:         n.Driver,
				Scope:          n.Scope,
				Internal:       n.Internal,
				Attachable:     n.Attachable,
				Created:        n.Created.Format("2006-01-02T15:04:05Z07:00"),
				Containers:     count,
				InUse:          count > 0,
				ContainerNames: names,
				Labels:         n.Labels,
			})
		}

		// Network modes in use but absent from NetworkList (rootless Podman uses
		// "pasta", "slirp4netns", "host", "none", or a custom network name).
		for name, names := range usage {
			if known[name] {
				continue
			}
			out = append(out, NetworkInfo{
				Name:           name,
				Driver:         "mode",
				Scope:          "local",
				Containers:     len(names),
				InUse:          true,
				ContainerNames: names,
				Virtual:        true,
			})
		}

		sort.Slice(out, func(i, j int) bool {
			if out[i].InUse != out[j].InUse {
				return out[i].InUse
			}
			return out[i].Name < out[j].Name
		})
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /api/docker/networks", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name       string `json:"name"`
			Driver     string `json:"driver"`
			Internal   bool   `json:"internal"`
			Attachable bool   `json:"attachable"`
			Subnet     string `json:"subnet"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			http.Error(w, "network name is required", http.StatusBadRequest)
			return
		}
		opts := network.CreateOptions{
			Driver:     req.Driver,
			Internal:   req.Internal,
			Attachable: req.Attachable,
			Labels:     map[string]string{"managed-by": "homelab-guard"},
		}
		if strings.TrimSpace(req.Subnet) != "" {
			opts.IPAM = &network.IPAM{Config: []network.IPAMConfig{{Subnet: req.Subnet}}}
		}
		res, err := dockerCli.NetworkCreate(r.Context(), strings.TrimSpace(req.Name), opts)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": res.ID, "name": req.Name})
	})

	mux.HandleFunc("DELETE /api/docker/networks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := dockerCli.NetworkRemove(r.Context(), r.PathValue("id")); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /api/docker/networks/prune", func(w http.ResponseWriter, r *http.Request) {
		report, err := dockerCli.NetworksPrune(r.Context(), filters.NewArgs())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": report.NetworksDeleted})
	})

	// ---------------- Images ----------------

	mux.HandleFunc("GET /api/docker/images", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, listImages(r.Context(), dockerCli))
	})

	mux.HandleFunc("DELETE /api/docker/images/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		force := r.URL.Query().Get("force") == "true"

		// Removing an image that a container uses is destructive on Podman: its
		// --force also removes those containers. Refuse unless explicitly forced.
		if !force {
			if inUse := containerImageIDs(r.Context(), dockerCli); inUse[id] {
				http.Error(w,
					"image is used by a running or stopped container; retry with ?force=true to remove it (this also removes the container)",
					http.StatusConflict)
				return
			}
		}

		if _, err := dockerCli.ImageRemove(r.Context(), id, image.RemoveOptions{Force: true}); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /api/docker/images/prune", func(w http.ResponseWriter, r *http.Request) {
		deleted, reclaimed := pruneUnusedImages(r.Context(), dockerCli)
		writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "space_reclaimed": reclaimed})
	})

	// ---------------- Volumes (container scratch data) ----------------

	mux.HandleFunc("GET /api/docker/volumes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, listVolumes(r.Context(), dockerCli))
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
		vol, err := dockerCli.VolumeCreate(r.Context(), volume.CreateOptions{
			Name:   strings.TrimSpace(req.Name),
			Driver: req.Driver,
			Labels: map[string]string{"managed-by": "homelab-guard"},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusCreated, vol)
	})

	mux.HandleFunc("DELETE /api/docker/volumes/{name}", func(w http.ResponseWriter, r *http.Request) {
		force := r.URL.Query().Get("force") == "true"
		if err := dockerCli.VolumeRemove(r.Context(), r.PathValue("name"), force); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /api/docker/volumes/prune", func(w http.ResponseWriter, r *http.Request) {
		deleted := []string{}
		var reclaimed int64
		for _, v := range listVolumes(r.Context(), dockerCli) {
			if v.InUse {
				continue
			}
			if err := dockerCli.VolumeRemove(r.Context(), v.Name, true); err == nil {
				deleted = append(deleted, v.Name)
				reclaimed += v.Size
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "space_reclaimed": reclaimed})
	})

	// ---------------- Disk usage summary ----------------

	mux.HandleFunc("GET /api/docker/disk-usage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, diskUsage(r.Context(), dockerCli))
	})

	mux.HandleFunc("POST /api/docker/build-cache/prune", func(w http.ResponseWriter, r *http.Request) {
		report, err := dockerCli.BuildCachePrune(r.Context(), dockerTypes.BuildCachePruneOptions{All: true})
		if err != nil {
			// Podman's Docker-compatible API has no build-cache endpoint (404).
			// Report it as unsupported rather than as a failure.
			if errdefs.IsNotFound(err) || strings.Contains(strings.ToLower(err.Error()), "not found") {
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

// pruneUnusedImages removes every image not referenced by a container. Removal
// runs in several passes because deleting a layer's child can surface its parent
// as a new unreferenced image. The reclaimed space is measured on the engine's
// unique layer storage, not by summing per-image sizes (which share layers).
func pruneUnusedImages(ctx context.Context, dockerCli *client.Client) ([]string, int64) {
	before := diskUsage(ctx, dockerCli).ImagesSize

	deleted := []string{}
	for pass := 0; pass < 8; pass++ {
		progress := false
		for _, img := range listImages(ctx, dockerCli) {
			if img.InUse {
				continue
			}
			if _, err := dockerCli.ImageRemove(ctx, img.ID, image.RemoveOptions{Force: true}); err == nil {
				deleted = append(deleted, img.ShortID)
				progress = true
			}
		}
		if !progress {
			break
		}
	}

	after := diskUsage(ctx, dockerCli).ImagesSize
	reclaimed := before - after
	if reclaimed < 0 {
		reclaimed = 0
	}
	return deleted, reclaimed
}

// networkUsage maps each network (or network mode) to the containers using it.
func networkUsage(ctx context.Context, dockerCli *client.Client) map[string][]string {
	usage := map[string][]string{}
	list, err := dockerCli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return usage
	}
	for _, c := range list {
		insp, err := dockerCli.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		for _, net := range containerNetworkNames(insp) {
			usage[net] = append(usage[net], name)
		}
	}
	for _, names := range usage {
		sort.Strings(names)
	}
	return usage
}

// listImages returns images annotated with whether a container uses them.
func listImages(ctx context.Context, dockerCli *client.Client) []ImageInfo {
	images, err := dockerCli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return []ImageInfo{}
	}
	inUse := containerImageIDs(ctx, dockerCli)

	out := make([]ImageInfo, 0, len(images))
	for _, img := range images {
		short := strings.TrimPrefix(img.ID, "sha256:")
		if len(short) > 12 {
			short = short[:12]
		}
		tags := img.RepoTags
		dangling := len(tags) == 0
		allNone := true
		for _, t := range tags {
			if t != "<none>:<none>" {
				allNone = false
				break
			}
		}
		if allNone {
			dangling = true
		}
		out = append(out, ImageInfo{
			ID:         img.ID,
			ShortID:    short,
			Tags:       tags,
			Size:       img.Size,
			Created:    img.Created,
			Dangling:   dangling,
			InUse:      inUse[img.ID],
			Containers: img.Containers,
		})
	}
	// Unused first, then newest.
	sort.Slice(out, func(i, j int) bool {
		if out[i].InUse != out[j].InUse {
			return !out[i].InUse
		}
		return out[i].Created > out[j].Created
	})
	return out
}

// containerImageIDs returns the set of image IDs referenced by any container.
func containerImageIDs(ctx context.Context, dockerCli *client.Client) map[string]bool {
	used := map[string]bool{}
	list, err := dockerCli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return used
	}
	for _, c := range list {
		used[c.ImageID] = true
		used[c.Image] = true
	}
	return used
}

// listVolumes returns volumes annotated with usage (size / reference count).
func listVolumes(ctx context.Context, dockerCli *client.Client) []VolumeInfo {
	usage, err := dockerCli.DiskUsage(ctx, dockerTypes.DiskUsageOptions{Types: []dockerTypes.DiskUsageObject{dockerTypes.VolumeObject}})
	if err == nil && len(usage.Volumes) > 0 {
		out := make([]VolumeInfo, 0, len(usage.Volumes))
		for _, v := range usage.Volumes {
			info := VolumeInfo{
				Name:       v.Name,
				Driver:     v.Driver,
				Mountpoint: v.Mountpoint,
				CreatedAt:  v.CreatedAt,
				Scope:      v.Scope,
			}
			if v.UsageData != nil {
				info.Size = v.UsageData.Size
				info.RefCount = v.UsageData.RefCount
			}
			info.InUse = info.RefCount > 0
			out = append(out, info)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].InUse != out[j].InUse {
				return !out[i].InUse
			}
			return out[i].Name < out[j].Name
		})
		return out
	}

	// Fallback: plain listing plus our own mount inspection.
	resp, err := dockerCli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return []VolumeInfo{}
	}
	mounted := mountedVolumes(ctx, dockerCli)
	out := make([]VolumeInfo, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		if v == nil {
			continue
		}
		out = append(out, VolumeInfo{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			CreatedAt:  v.CreatedAt,
			Scope:      v.Scope,
			InUse:      mounted[v.Name],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// mountedVolumes returns the names of volumes currently mounted by a container.
func mountedVolumes(ctx context.Context, dockerCli *client.Client) map[string]bool {
	used := map[string]bool{}
	list, err := dockerCli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return used
	}
	for _, c := range list {
		insp, err := dockerCli.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}
		for _, m := range insp.Mounts {
			if m.Type == "volume" && m.Name != "" {
				used[m.Name] = true
			}
		}
	}
	return used
}

func diskUsage(ctx context.Context, dockerCli *client.Client) DiskUsageInfo {
	var info DiskUsageInfo
	du, err := dockerCli.DiskUsage(ctx, dockerTypes.DiskUsageOptions{})
	if err != nil {
		return info
	}

	// LayersSize is the engine's unique layer storage: summing per-image sizes
	// would count shared layers once per image and wildly overstate the total.
	info.ImagesSize = du.LayersSize
	// Only images returned by ImageList can actually be removed; /system/df also
	// reports intermediate layers that are pinned by their children.
	var unusedImagesSize int64
	for _, img := range listImages(ctx, dockerCli) {
		if !img.InUse {
			unusedImagesSize += img.Size
		}
	}
	// Reclaimable image space can never exceed the total unique storage.
	info.ImagesReclaim = unusedImagesSize
	if info.ImagesReclaim > info.ImagesSize {
		info.ImagesReclaim = info.ImagesSize
	}
	for _, c := range du.Containers {
		if c == nil {
			continue
		}
		info.ContainersCount++
		info.ContainersSize += c.SizeRw
	}
	for _, v := range du.Volumes {
		if v == nil {
			continue
		}
		var size int64
		var refs int64
		if v.UsageData != nil {
			size = v.UsageData.Size
			refs = v.UsageData.RefCount
		}
		info.VolumesSize += size
		if refs == 0 {
			info.VolumesReclaim += size
		}
	}
	for _, bc := range du.BuildCache {
		if bc == nil {
			continue
		}
		info.BuildCacheCount++
		info.BuildCacheSize += bc.Size
		if !bc.InUse {
			info.TotalReclaim += bc.Size
		}
	}
	info.TotalReclaim += info.ImagesReclaim + info.VolumesReclaim
	return info
}
