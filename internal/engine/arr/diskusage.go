package arr

import (
	"path"
	"strings"
)

// Mount is a disk reported by an arr's diskspace endpoint.
type Mount struct {
	Path  string
	Free  int64
	Total int64
}

// UsedPercent returns the used space of the mount in percent.
func (m Mount) UsedPercent() float64 {
	return float64(m.Total-m.Free) / float64(m.Total) * 100
}

// RootFolderUsage maps each root folder to the usage percentage of the mount it
// lives on. A root folder is matched to the mount with the longest path prefix.
// Root folders without a usable mount are omitted.
func RootFolderUsage(rootFolders []string, mounts []Mount) map[string]float64 {
	usage := make(map[string]float64, len(rootFolders))
	for _, root := range rootFolders {
		if m, ok := mountFor(root, mounts); ok {
			usage[root] = m.UsedPercent()
		}
	}
	return usage
}

func mountFor(root string, mounts []Mount) (Mount, bool) {
	root = path.Clean(root)
	var best Mount
	found := false
	for _, m := range mounts {
		if m.Total <= 0 {
			continue
		}
		mp := path.Clean(m.Path)
		if !isUnder(root, mp) {
			continue
		}
		if !found || len(mp) > len(path.Clean(best.Path)) {
			best, found = m, true
		}
	}
	return best, found
}

// isUnder reports whether p is equal to or inside dir (both cleaned).
func isUnder(p, dir string) bool {
	if dir == "/" {
		return strings.HasPrefix(p, "/")
	}
	return p == dir || strings.HasPrefix(p, dir+"/")
}
