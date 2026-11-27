// Package dockerops implements the Docker/Podman operations exposed by Homelab
// Guard (networks, images, volumes, disk usage). It is used directly by the
// server for the local engine and by the remote agent, so both report exactly
// the same data to the UI.
package dockerops

import (
	"context"
	"sort"
	"strings"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/overyanosh/homelab-guard/backend/internal/ctrinfo"
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
	Virtual        bool              `json:"virtual,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
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

// VolumeInfo is the UI-facing view of a volume.
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

// NetworkCreateRequest is the payload accepted by CreateNetwork.
type NetworkCreateRequest struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Internal   bool   `json:"internal"`
	Attachable bool   `json:"attachable"`
	Subnet     string `json:"subnet"`
}

// Networks lists networks annotated with their real usage.
func Networks(ctx context.Context, cli *client.Client) ([]NetworkInfo, error) {
	nets, err := cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, err
	}
	usage := networkUsage(ctx, cli)

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
	return out, nil
}

// CreateNetwork creates a network and returns its ID.
func CreateNetwork(ctx context.Context, cli *client.Client, req NetworkCreateRequest) (string, error) {
	opts := network.CreateOptions{
		Driver:     req.Driver,
		Internal:   req.Internal,
		Attachable: req.Attachable,
		Labels:     map[string]string{"managed-by": "homelab-guard"},
	}
	if strings.TrimSpace(req.Subnet) != "" {
		opts.IPAM = &network.IPAM{Config: []network.IPAMConfig{{Subnet: req.Subnet}}}
	}
	res, err := cli.NetworkCreate(ctx, strings.TrimSpace(req.Name), opts)
	if err != nil {
		return "", err
	}
	return res.ID, nil
}

// RemoveNetwork deletes a network.
func RemoveNetwork(ctx context.Context, cli *client.Client, id string) error {
	return cli.NetworkRemove(ctx, id)
}

// PruneNetworks removes unused networks.
func PruneNetworks(ctx context.Context, cli *client.Client) ([]string, error) {
	report, err := cli.NetworksPrune(ctx, filters.NewArgs())
	if err != nil {
		return nil, err
	}
	return report.NetworksDeleted, nil
}

// Images lists images annotated with usage.
func Images(ctx context.Context, cli *client.Client) []ImageInfo {
	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return []ImageInfo{}
	}
	inUse := containerImageIDs(ctx, cli)

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
	sort.Slice(out, func(i, j int) bool {
		if out[i].InUse != out[j].InUse {
			return !out[i].InUse
		}
		return out[i].Created > out[j].Created
	})
	return out
}

// RemoveImage deletes an image. When force is false, images used by a container
// are refused (Podman's force removal also deletes those containers).
func RemoveImage(ctx context.Context, cli *client.Client, id string, force bool) error {
	if !force {
		if containerImageIDs(ctx, cli)[id] {
			return errImageInUse
		}
	}
	_, err := cli.ImageRemove(ctx, id, image.RemoveOptions{Force: true})
	return err
}

var errImageInUse = &imageInUseError{}

type imageInUseError struct{}

func (e *imageInUseError) Error() string {
	return "image is used by a running or stopped container; retry with force=true to remove it (this also removes the container)"
}

// PruneImages removes every image not referenced by a container, in several
// passes (removing a layer's child can surface its parent). The reclaimed space
// is measured on the engine's unique layer storage.
func PruneImages(ctx context.Context, cli *client.Client) ([]string, int64) {
	before := DiskUsage(ctx, cli).ImagesSize

	deleted := []string{}
	for pass := 0; pass < 8; pass++ {
		progress := false
		for _, img := range Images(ctx, cli) {
			if img.InUse {
				continue
			}
			if _, err := cli.ImageRemove(ctx, img.ID, image.RemoveOptions{Force: true}); err == nil {
				deleted = append(deleted, img.ShortID)
				progress = true
			}
		}
		if !progress {
			break
		}
	}

	after := DiskUsage(ctx, cli).ImagesSize
	reclaimed := before - after
	if reclaimed < 0 {
		reclaimed = 0
	}
	return deleted, reclaimed
}

// Volumes lists volumes annotated with usage.
func Volumes(ctx context.Context, cli *client.Client) []VolumeInfo {
	usage, err := cli.DiskUsage(ctx, dockerTypes.DiskUsageOptions{Types: []dockerTypes.DiskUsageObject{dockerTypes.VolumeObject}})
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

	resp, err := cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return []VolumeInfo{}
	}
	mounted := mountedVolumes(ctx, cli)
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

// CreateVolume creates a named volume.
func CreateVolume(ctx context.Context, cli *client.Client, name, driver string) error {
	_, err := cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:   strings.TrimSpace(name),
		Driver: driver,
		Labels: map[string]string{"managed-by": "homelab-guard"},
	})
	return err
}

// RemoveVolume deletes a volume.
func RemoveVolume(ctx context.Context, cli *client.Client, name string, force bool) error {
	return cli.VolumeRemove(ctx, name, force)
}

// PruneVolumes removes unused volumes and reports the reclaimed space.
func PruneVolumes(ctx context.Context, cli *client.Client) ([]string, int64) {
	deleted := []string{}
	var reclaimed int64
	for _, v := range Volumes(ctx, cli) {
		if v.InUse {
			continue
		}
		if err := cli.VolumeRemove(ctx, v.Name, true); err == nil {
			deleted = append(deleted, v.Name)
			reclaimed += v.Size
		}
	}
	return deleted, reclaimed
}

// DiskUsage summarises storage usage.
func DiskUsage(ctx context.Context, cli *client.Client) DiskUsageInfo {
	var info DiskUsageInfo
	du, err := cli.DiskUsage(ctx, dockerTypes.DiskUsageOptions{})
	if err != nil {
		return info
	}
	info.ImagesSize = du.LayersSize
	var unusedImagesSize int64
	for _, img := range Images(ctx, cli) {
		if !img.InUse {
			unusedImagesSize += img.Size
		}
	}
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
		var size, refs int64
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

// --- internal helpers -------------------------------------------------------

func networkUsage(ctx context.Context, cli *client.Client) map[string][]string {
	usage := map[string][]string{}
	list, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return usage
	}
	for _, c := range list {
		insp, err := cli.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		for _, net := range ctrinfo.NetworkNames(insp) {
			usage[net] = append(usage[net], name)
		}
	}
	for _, names := range usage {
		sort.Strings(names)
	}
	return usage
}

func containerImageIDs(ctx context.Context, cli *client.Client) map[string]bool {
	used := map[string]bool{}
	list, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return used
	}
	for _, c := range list {
		used[c.ImageID] = true
		used[c.Image] = true
	}
	return used
}

func mountedVolumes(ctx context.Context, cli *client.Client) map[string]bool {
	used := map[string]bool{}
	list, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return used
	}
	for _, c := range list {
		insp, err := cli.ContainerInspect(ctx, c.ID)
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
