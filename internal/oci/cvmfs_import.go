package oci

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cclient "j5.nz/cc/client"
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
	return intcvmfs.FormatTarget(intcvmfs.Target{
		Mirror: parsed.Mirror,
		Repo:   parsed.Repo,
		Path:   joinedPath,
		Remote: true,
	})
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

func prefetchCVMFSFiles(ctx context.Context, client *intcvmfs.Client, nodes []indexedNode, workers int, contentsPath string, imageName string, report func(cclient.ProgressEvent)) ([]indexedNode, error) {
	type prefetchTarget struct {
		nodeIndex int
		target    string
		size      uint64
		offset    uint64
	}

	workers = resolvePrefetchWorkers(workers)
	packedNodes := append([]indexedNode(nil), nodes...)
	targets := make([]prefetchTarget, 0, len(nodes))
	var totalBytes uint64
	for i, node := range packedNodes {
		if node.Kind != indexedKindFile || strings.TrimSpace(node.CVMFSTarget) == "" {
			continue
		}
		packedNodes[i].Packed = true
		packedNodes[i].PackedOffset = totalBytes
		targets = append(targets, prefetchTarget{
			nodeIndex: i,
			target:    node.CVMFSTarget,
			size:      node.Size,
			offset:    totalBytes,
		})
		totalBytes += node.Size
	}
	if len(targets) == 0 {
		return packedNodes, nil
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].size == targets[j].size {
			return targets[i].target < targets[j].target
		}
		return targets[i].size > targets[j].size
	})

	startedAt := time.Now()
	fmt.Fprintf(os.Stderr, "ccx3-prefetch: starting prefetch of %d files (%s) with %d workers\n", len(targets), formatPrefetchBytes(totalBytes), workers)
	reportPullProgress(report, cclient.ProgressEvent{
		Status:          "prefetching",
		Artifact:        imageName,
		Blob:            "rootfs.contents",
		BytesDownloaded: 0,
		BytesTotal:      int64(totalBytes),
	})
	if err := os.MkdirAll(filepath.Dir(contentsPath), 0o755); err != nil {
		return nil, err
	}
	tmpPath := contentsPath + ".tmp"
	if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	contentsFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = contentsFile.Close()
		_ = os.Remove(tmpPath)
	}()
	if totalBytes > 0 {
		if err := contentsFile.Truncate(int64(totalBytes)); err != nil {
			return nil, err
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workCh := make(chan prefetchTarget)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	var completedFiles atomic.Uint64
	var completedBytes atomic.Uint64

	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		defer close(progressDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				logPrefetchProgress(startedAt, len(targets), totalBytes, completedFiles.Load(), completedBytes.Load())
				reportPrefetchProgress(report, imageName, startedAt, totalBytes, completedBytes.Load())
			}
		}
	}()

	worker := func() {
		defer wg.Done()
		for target := range workCh {
			written, err := client.WriteFileTo(target.target, io.NewOffsetWriter(contentsFile, int64(target.offset)))
			if err == nil && written != target.size {
				err = fmt.Errorf("packed %q wrote %d bytes, want %d", target.target, written, target.size)
			}
			if err != nil {
				select {
				case errCh <- fmt.Errorf("pack %q: %w", target.target, err):
				default:
				}
				cancel()
				return
			}
			completedFiles.Add(1)
			completedBytes.Add(written)
		}
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}

sendLoop:
	for _, target := range targets {
		select {
		case <-ctx.Done():
			break sendLoop
		case workCh <- target:
		}
	}
	close(workCh)
	wg.Wait()
	cancel()
	<-progressDone

	select {
	case err := <-errCh:
		logPrefetchProgress(startedAt, len(targets), totalBytes, completedFiles.Load(), completedBytes.Load())
		reportPrefetchProgress(report, imageName, startedAt, totalBytes, completedBytes.Load())
		return nil, err
	default:
		logPrefetchProgress(startedAt, len(targets), totalBytes, completedFiles.Load(), completedBytes.Load())
		reportPrefetchProgress(report, imageName, startedAt, totalBytes, completedBytes.Load())
		fmt.Fprintf(os.Stderr, "ccx3-prefetch: completed in %s\n", time.Since(startedAt).Round(time.Second))
		if err := contentsFile.Close(); err != nil {
			return nil, err
		}
		if err := os.Rename(tmpPath, contentsPath); err != nil {
			return nil, err
		}
		reportPullProgress(report, cclient.ProgressEvent{
			Status:          "downloaded",
			Artifact:        imageName,
			Blob:            filepath.Base(contentsPath),
			BytesDownloaded: int64(totalBytes),
			BytesTotal:      int64(totalBytes),
			Progress:        1,
		})
		return packedNodes, nil
	}
}

func resolvePrefetchWorkers(requested int) int {
	cpus := runtime.NumCPU()
	if cpus < 1 {
		cpus = 1
	}
	if requested > 0 {
		if requested > cpus {
			return cpus
		}
		return requested
	}
	defaultWorkers := cpus / 2
	if defaultWorkers < 1 {
		defaultWorkers = 1
	}
	if defaultWorkers > 4 {
		defaultWorkers = 4
	}
	return defaultWorkers
}

func reportPrefetchProgress(report func(cclient.ProgressEvent), imageName string, startedAt time.Time, totalBytes, completedBytes uint64) {
	if report == nil {
		return
	}
	elapsed := time.Since(startedAt)
	if elapsed <= 0 {
		elapsed = time.Second
	}
	rate := float64(completedBytes) / elapsed.Seconds()
	remainingBytes := uint64(0)
	if totalBytes > completedBytes {
		remainingBytes = totalBytes - completedBytes
	}
	etaSeconds := 0.0
	if remainingBytes > 0 && rate > 0 {
		etaSeconds = float64(remainingBytes) / rate
	}
	progress := 0.0
	if totalBytes > 0 {
		progress = float64(completedBytes) / float64(totalBytes)
	}
	reportPullProgress(report, cclient.ProgressEvent{
		Status:             "downloading",
		Artifact:           imageName,
		Blob:               "rootfs.contents",
		Progress:           progress,
		BytesDownloaded:    int64(completedBytes),
		BytesTotal:         int64(totalBytes),
		RateBytesPerSecond: rate,
		ETASeconds:         etaSeconds,
	})
}

func logPrefetchProgress(startedAt time.Time, totalFiles int, totalBytes, completedFiles, completedBytes uint64) {
	elapsed := time.Since(startedAt)
	if elapsed <= 0 {
		elapsed = time.Second
	}

	rate := float64(completedBytes) / elapsed.Seconds()
	remainingBytes := uint64(0)
	if totalBytes > completedBytes {
		remainingBytes = totalBytes - completedBytes
	}

	eta := "unknown"
	if remainingBytes == 0 {
		eta = "0s"
	} else if rate > 0 {
		eta = (time.Duration(float64(remainingBytes)/rate) * time.Second).Round(time.Second).String()
	}

	fmt.Fprintf(
		os.Stderr,
		"ccx3-prefetch: %d/%d files, %s/%s, rate %s/s, eta %s\n",
		completedFiles,
		totalFiles,
		formatPrefetchBytes(completedBytes),
		formatPrefetchBytes(totalBytes),
		formatPrefetchBytes(uint64(rate)),
		eta,
	)
}

func formatPrefetchBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
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
