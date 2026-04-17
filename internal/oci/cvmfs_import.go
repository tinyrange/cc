package oci

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	intcvmfs "j5.nz/cc/internal/cvmfs"
	"j5.nz/cc/internal/fsmeta"
)

func cvmfsCacheDir(root string) string {
	return filepath.Join(root, "_cvmfs_cache")
}

func joinCVMFSTarget(base, rel string) (string, error) {
	parsed, err := intcvmfs.ParseTarget(base)
	if err != nil {
		return "", err
	}
	joinedPath := path.Join(parsed.Path, rel)
	if !parsed.Remote {
		if joinedPath == "/" {
			return parsed.LocalPath, nil
		}
		return filepath.Join(parsed.LocalPath, filepath.FromSlash(strings.TrimPrefix(joinedPath, "/"))), nil
	}
	return strings.TrimRight(parsed.Mirror, "/") + "/" + parsed.Repo + joinedPath, nil
}

func resolveCVMFSRootTarget(client *intcvmfs.Client, source string) (string, bool, error) {
	if _, err := client.ReadDir(source); err == nil {
		parsed, parseErr := intcvmfs.ParseTarget(source)
		if parseErr != nil {
			return "", false, parseErr
		}
		base := path.Base(parsed.Path)
		if strings.HasSuffix(base, ".simg") {
			return source, true, nil
		}
		rootTarget, err := joinCVMFSTarget(source, base+".simg")
		if err != nil {
			return "", false, err
		}
		if _, err := client.ReadDir(rootTarget); err == nil {
			return rootTarget, true, nil
		}
		return "", false, fmt.Errorf("resolve cvmfs container root: %q does not contain %q", source, base+".simg")
	}
	return "", false, fmt.Errorf("read cvmfs container directory: %w", os.ErrNotExist)
}

func buildCVMFSDirectoryIndex(client *intcvmfs.Client, rootTarget string) ([]indexedNode, map[string]fsmeta.Entry, string, error) {
	parsed, err := intcvmfs.ParseTarget(rootTarget)
	if err != nil {
		return nil, nil, "", err
	}
	rootPath := parsed.Path
	nodes := make([]indexedNode, 0, 1024)
	meta := make(map[string]fsmeta.Entry, 1024)
	fileTargets := map[string]string{}

	err = client.Walk(rootTarget, func(entry intcvmfs.WalkEntry) error {
		guestPath := cvmfsGuestPath(rootPath, entry.Path)
		mode := fsmeta.LinuxModeFromFileMode(entry.Mode)
		node := indexedNode{
			Path:      guestPath,
			Mode:      mode,
			UID:       entry.UID,
			GID:       entry.GID,
			RDev:      entry.RDev,
			ModTimeNS: entry.ModTime.UnixNano(),
		}
		meta[guestPath] = fsmeta.Entry{
			UID:  entry.UID,
			GID:  entry.GID,
			Mode: mode,
			RDev: entry.RDev,
		}

		switch {
		case entry.Mode.IsDir():
			node.Kind = indexedKindDir
		case entry.Mode&fs.ModeSymlink != 0:
			node.Kind = indexedKindSymlink
			node.LinkTarget = entry.Symlink
		default:
			node.Kind = indexedKindFile
			node.Size = uint64(entry.Size)
			node.CVMFSTarget, err = joinCVMFSTarget(rootTarget, strings.TrimPrefix(guestPath, "/"))
			if err != nil {
				return err
			}
			fileTargets[guestPath] = node.CVMFSTarget
		}

		nodes = append(nodes, node)
		return nil
	})
	if err != nil {
		return nil, nil, "", err
	}

	arch, err := detectCVMFSArchitecture(client, fileTargets)
	if err != nil {
		return nil, nil, "", err
	}
	return nodes, meta, arch, nil
}

func cvmfsIndexCacheKey(rootHash, rootTarget string) string {
	sum := sha256.Sum256([]byte(rootHash + "\n" + rootTarget))
	return hex.EncodeToString(sum[:])
}

func cvmfsIndexCacheDir(cacheDir, rootHash, rootTarget string) string {
	return filepath.Join(cacheDir, "indexes", cvmfsIndexCacheKey(rootHash, rootTarget))
}

func loadCVMFSDirectoryIndexCache(cacheDir, rootHash, rootTarget string) ([]indexedNode, map[string]fsmeta.Entry, string, bool, error) {
	if strings.TrimSpace(cacheDir) == "" || strings.TrimSpace(rootHash) == "" {
		return nil, nil, "", false, nil
	}
	dir := cvmfsIndexCacheDir(cacheDir, rootHash, rootTarget)
	indexBuf, err := os.ReadFile(filepath.Join(dir, "rootfs.index.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, "", false, nil
		}
		return nil, nil, "", false, err
	}
	nodes, err := decodeFSIndex(indexBuf)
	if err != nil {
		return nil, nil, "", false, err
	}
	metaBuf, err := os.ReadFile(filepath.Join(dir, "rootfs.metadata.json"))
	if err != nil {
		return nil, nil, "", false, err
	}
	var entries map[string]fsmeta.Entry
	if err := json.Unmarshal(metaBuf, &entries); err != nil {
		return nil, nil, "", false, err
	}
	archBuf, err := os.ReadFile(filepath.Join(dir, "architecture"))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, "", false, err
	}
	return nodes, entries, strings.TrimSpace(string(archBuf)), true, nil
}

func saveCVMFSDirectoryIndexCache(cacheDir, rootHash, rootTarget string, nodes []indexedNode, entries map[string]fsmeta.Entry, arch string) error {
	if strings.TrimSpace(cacheDir) == "" || strings.TrimSpace(rootHash) == "" {
		return nil
	}
	dir := cvmfsIndexCacheDir(cacheDir, rootHash, rootTarget)
	tmpDir := dir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return err
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	indexBuf, err := encodeIndexedNodes(nodes)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "rootfs.index.json"), indexBuf, 0o644); err != nil {
		return err
	}
	metaBuf, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "rootfs.metadata.json"), metaBuf, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "architecture"), []byte(strings.TrimSpace(arch)+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpDir, dir)
}

func cvmfsGuestPath(rootPath, entryPath string) string {
	rootPath = path.Clean("/" + strings.TrimPrefix(rootPath, "/"))
	entryPath = path.Clean("/" + strings.TrimPrefix(entryPath, "/"))
	if entryPath == rootPath {
		return "/"
	}
	return "/" + strings.TrimPrefix(strings.TrimPrefix(entryPath, rootPath), "/")
}

func detectCVMFSArchitecture(client *intcvmfs.Client, fileTargets map[string]string) (string, error) {
	for _, guestPath := range []string{"/bin/sh", "/bin/bash", "/usr/bin/env", "/usr/bin/bash", "/bin/busybox"} {
		target := fileTargets[guestPath]
		if target == "" {
			continue
		}
		data, _, err := client.ReadFileRange(target, 0, 64)
		if err != nil {
			return "", err
		}
		if arch := detectELFArchitecture(data); arch != "" {
			return arch, nil
		}
	}
	return "", nil
}

func detectELFArchitecture(data []byte) string {
	if len(data) < 20 || string(data[:4]) != "\x7fELF" {
		return ""
	}
	var machine uint16
	switch data[5] {
	case 1:
		machine = binary.LittleEndian.Uint16(data[18:20])
	case 2:
		machine = binary.BigEndian.Uint16(data[18:20])
	default:
		return ""
	}
	switch machine {
	case 0x3e:
		return "amd64"
	case 0xb7:
		return "arm64"
	default:
		return ""
	}
}
