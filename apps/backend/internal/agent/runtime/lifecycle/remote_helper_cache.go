package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// PruneRemoteHelperCache retains the running release and its two previous
// versions. Cleanup is skipped unless the caller has positively inventoried
// every managed container that can still reference a helper mount.
func PruneRemoteHelperCache(cacheRoot, currentVersion string, pinnedPaths []string, inventoryComplete bool) ([]string, error) {
	if !inventoryComplete {
		return nil, nil
	}
	if _, err := parseRemoteHelperVersion(currentVersion); err != nil {
		return nil, fmt.Errorf("invalid current remote helper version: %w", err)
	}
	versions, err := readRemoteHelperCacheVersions(cacheRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	keep := remoteHelperVersionsToKeep(versions, currentVersion)
	protected, err := absolutePinnedHelperPaths(pinnedPaths)
	if err != nil {
		return nil, err
	}
	return removeUnusedRemoteHelperVersions(cacheRoot, versions, keep, protected)
}

func readRemoteHelperCacheVersions(cacheRoot string) ([]string, error) {
	entries, err := os.ReadDir(cacheRoot)
	if os.IsNotExist(err) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, fmt.Errorf("read remote helper cache: %w", err)
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := parseRemoteHelperVersion(entry.Name()); err == nil {
				versions = append(versions, entry.Name())
			}
		}
	}
	sort.Slice(versions, func(i, j int) bool { return compareRemoteHelperVersions(versions[i], versions[j]) > 0 })
	return versions, nil
}

func remoteHelperVersionsToKeep(versions []string, currentVersion string) map[string]struct{} {
	keep := map[string]struct{}{currentVersion: {}}
	previous := 0
	for _, version := range versions {
		if compareRemoteHelperVersions(version, currentVersion) >= 0 {
			continue
		}
		if previous == 2 {
			break
		}
		keep[version] = struct{}{}
		previous++
	}
	return keep
}

func absolutePinnedHelperPaths(pinnedPaths []string) ([]string, error) {
	protected := make([]string, 0, len(pinnedPaths))
	for _, path := range pinnedPaths {
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve pinned helper path: %w", err)
		}
		protected = append(protected, filepath.Clean(absolute))
	}
	return protected, nil
}

func removeUnusedRemoteHelperVersions(cacheRoot string, versions []string, keep map[string]struct{}, protected []string) ([]string, error) {
	removed := make([]string, 0)
	for _, version := range versions {
		if _, ok := keep[version]; ok {
			continue
		}
		versionPath := filepath.Join(cacheRoot, version)
		if cacheVersionContainsPinnedPath(versionPath, protected) {
			continue
		}
		if err := os.RemoveAll(versionPath); err != nil {
			return removed, fmt.Errorf("remove unused remote helper cache version %s: %w", version, err)
		}
		removed = append(removed, version)
	}
	return removed, nil
}

func cacheVersionContainsPinnedPath(versionPath string, pinned []string) bool {
	absoluteVersionPath, err := filepath.Abs(versionPath)
	if err != nil {
		return true
	}
	for _, path := range pinned {
		rel, err := filepath.Rel(absoluteVersionPath, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

type remoteHelperVersion struct {
	major int
	minor int
	patch int
	pre   string
}

func parseRemoteHelperVersion(version string) (remoteHelperVersion, error) {
	version, _ = strings.CutPrefix(version, "v")
	parts := strings.SplitN(version, "-", 2)
	numbers := strings.Split(parts[0], ".")
	if len(numbers) != 3 {
		return remoteHelperVersion{}, fmt.Errorf("version %q is not semantic", version)
	}
	values := [3]int{}
	for i, number := range numbers {
		value, err := strconv.Atoi(number)
		if err != nil || value < 0 {
			return remoteHelperVersion{}, fmt.Errorf("version %q is not semantic", version)
		}
		values[i] = value
	}
	pre := ""
	if len(parts) == 2 {
		pre = parts[1]
		if pre == "" || strings.ContainsAny(pre, `/\\ `) {
			return remoteHelperVersion{}, fmt.Errorf("version %q has invalid prerelease", version)
		}
	}
	return remoteHelperVersion{major: values[0], minor: values[1], patch: values[2], pre: pre}, nil
}

func compareRemoteHelperVersions(left, right string) int {
	l, lerr := parseRemoteHelperVersion(left)
	r, rerr := parseRemoteHelperVersion(right)
	if lerr != nil || rerr != nil {
		return strings.Compare(left, right)
	}
	for _, pair := range [][2]int{{l.major, r.major}, {l.minor, r.minor}, {l.patch, r.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if l.pre == r.pre {
		return 0
	}
	if l.pre == "" {
		return 1
	}
	if r.pre == "" {
		return -1
	}
	return strings.Compare(l.pre, r.pre)
}
