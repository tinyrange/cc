package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	freebsdrootfs "j5.nz/cc/internal/freebsd/rootfs"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/managed/rootartifact"
	netbsdrootfs "j5.nz/cc/internal/netbsd/rootfs"
	openbsdrootfs "j5.nz/cc/internal/openbsd/rootfs"
)

const bsdKernelMediaType = "application/vnd.tinyrange.bsd.kernel.v1"
const customKernelFilePrefix = "file:"

type bsdOCIManifest struct {
	Layers []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
	} `json:"layers"`
}

func buildOpenBSDArtifact(ctx context.Context, cacheDir, arch, kernelFlavor string, network machine.NetworkSpec) (rootartifact.Artifact, error) {
	if artifact, ok, err := buildBSDOCIArtifact(ctx, "openbsd", openBSDOCITag(arch), cacheDir, arch, kernelFlavor, network); ok || err != nil {
		return artifact, err
	}
	rt, err := openbsdrootfs.BuildManagedRuntime(ctx, openbsdrootfs.Config{CacheDir: cacheDir, Arch: arch, Network: network})
	if err != nil {
		return rootartifact.Artifact{}, err
	}
	return bsdArtifactWithCustomKernel(rt.Artifact(), kernelFlavor)
}

func buildFreeBSDArtifact(ctx context.Context, cacheDir, arch, kernelFlavor string, network machine.NetworkSpec) (rootartifact.Artifact, error) {
	if artifact, ok, err := buildBSDOCIArtifact(ctx, "freebsd", freeBSDOCITag(arch), cacheDir, arch, kernelFlavor, network); ok || err != nil {
		return artifact, err
	}
	rt, err := freebsdrootfs.BuildManagedRuntime(ctx, freebsdrootfs.Config{CacheDir: cacheDir, Arch: arch, Network: network})
	if err != nil {
		return rootartifact.Artifact{}, err
	}
	return bsdArtifactWithCustomKernel(rt.Artifact(), kernelFlavor)
}

func buildNetBSDArtifact(ctx context.Context, cacheDir, arch, kernelFlavor string, network machine.NetworkSpec) (rootartifact.Artifact, error) {
	if artifact, ok, err := buildBSDOCIArtifact(ctx, "netbsd", netBSDOCITag(arch), cacheDir, arch, kernelFlavor, network); ok || err != nil {
		return artifact, err
	}
	rt, err := netbsdrootfs.BuildManagedRuntime(ctx, netbsdrootfs.Config{CacheDir: cacheDir, Arch: arch, Network: network})
	if err != nil {
		return rootartifact.Artifact{}, err
	}
	return bsdArtifactWithCustomKernel(rt.Artifact(), kernelFlavor)
}

func buildBSDOCIArtifact(ctx context.Context, family, tag, cacheDir, arch, kernelFlavor string, network machine.NetworkSpec) (rootartifact.Artifact, bool, error) {
	if strings.TrimSpace(os.Getenv("CC_BSD_OCI_DISABLE")) != "" {
		return rootartifact.Artifact{}, false, nil
	}
	repo := "tinyrange/cc-" + family
	rootLayer, kernel, err := ensureBSDOCIArtifact(ctx, filepath.Join(cacheDir, "oci"), repo, tag)
	if err != nil {
		return rootartifact.Artifact{}, false, err
	}
	kernel, err = bsdKernelBytes(kernelFlavor, kernel)
	if err != nil {
		return rootartifact.Artifact{}, false, err
	}
	switch family {
	case "openbsd":
		rt, err := openbsdrootfs.BuildManagedRuntimeFromOCI(ctx, openbsdrootfs.Config{CacheDir: cacheDir, Arch: arch, Network: network}, kernel, rootLayer)
		if err != nil {
			return rootartifact.Artifact{}, true, err
		}
		return rt.Artifact(), true, nil
	case "freebsd":
		rt, err := freebsdrootfs.BuildManagedRuntimeFromOCI(ctx, freebsdrootfs.Config{CacheDir: cacheDir, Arch: arch, Network: network}, kernel, rootLayer)
		if err != nil {
			return rootartifact.Artifact{}, true, err
		}
		return rt.Artifact(), true, nil
	case "netbsd":
		rt, err := netbsdrootfs.BuildManagedRuntimeFromOCI(ctx, netbsdrootfs.Config{CacheDir: cacheDir, Arch: arch, Network: network}, kernel, rootLayer)
		if err != nil {
			return rootartifact.Artifact{}, true, err
		}
		return rt.Artifact(), true, nil
	default:
		return rootartifact.Artifact{}, false, fmt.Errorf("unsupported BSD OCI family %q", family)
	}
}

func bsdArtifactWithCustomKernel(artifact rootartifact.Artifact, kernelFlavor string) (rootartifact.Artifact, error) {
	kernel, err := bsdKernelBytes(kernelFlavor, artifact.Kernel)
	if err != nil {
		if artifact.Cleanup != nil {
			_ = artifact.Cleanup()
		}
		return rootartifact.Artifact{}, err
	}
	artifact.Kernel = kernel
	return artifact, nil
}

func bsdKernelBytes(kernelFlavor string, packaged []byte) ([]byte, error) {
	kernelFlavor = strings.TrimSpace(kernelFlavor)
	if path, ok := strings.CutPrefix(kernelFlavor, customKernelFilePrefix); ok {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, fmt.Errorf("custom BSD kernel path is empty")
		}
		kernel, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read custom BSD kernel %s: %w", path, err)
		}
		if len(kernel) == 0 {
			return nil, fmt.Errorf("custom BSD kernel %s is empty", path)
		}
		return kernel, nil
	}
	return append([]byte(nil), packaged...), nil
}

func openBSDOCITag(arch string) string {
	if arch == "" {
		arch = "amd64"
	}
	if arch == "amd64" {
		return "7.9-amd64"
	}
	return "7.9-" + arch
}

func freeBSDOCITag(arch string) string {
	if arch == "" {
		arch = "amd64"
	}
	if arch == "amd64" {
		return "15.1-amd64"
	}
	return "15.1-" + arch
}

func netBSDOCITag(arch string) string {
	if arch == "" {
		arch = "amd64"
	}
	return "10.1-" + arch
}

func ensureBSDOCIArtifact(ctx context.Context, cacheDir, repo, tag string) (string, []byte, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", nil, err
	}
	manifestURL := "https://ghcr.io/v2/" + repo + "/manifests/" + tag
	data, err := registryGet(ctx, manifestURL, "application/vnd.oci.image.manifest.v1+json")
	if err != nil {
		return "", nil, err
	}
	var manifest bsdOCIManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", nil, fmt.Errorf("decode BSD OCI manifest: %w", err)
	}
	var rootDigest, kernelDigest string
	for _, layer := range manifest.Layers {
		if strings.Contains(layer.MediaType, "tar+gzip") {
			rootDigest = layer.Digest
		}
		if layer.MediaType == bsdKernelMediaType {
			kernelDigest = layer.Digest
		}
	}
	if rootDigest == "" || kernelDigest == "" {
		return "", nil, fmt.Errorf("BSD OCI manifest missing rootfs or kernel layer")
	}
	rootPath := filepath.Join(cacheDir, digestFile(rootDigest)+".tar.gz")
	if err := ensureRegistryBlob(ctx, repo, rootDigest, rootPath); err != nil {
		return "", nil, err
	}
	kernelPath := filepath.Join(cacheDir, digestFile(kernelDigest)+".kernel")
	if err := ensureRegistryBlob(ctx, repo, kernelDigest, kernelPath); err != nil {
		return "", nil, err
	}
	kernel, err := os.ReadFile(kernelPath)
	if err != nil {
		return "", nil, err
	}
	return rootPath, kernel, nil
}

func ensureRegistryBlob(ctx context.Context, repo, digest, target string) error {
	if st, err := os.Stat(target); err == nil && st.Size() > 0 {
		return nil
	}
	resp, err := registryDo(ctx, "https://ghcr.io/v2/"+repo+"/blobs/"+digest, "application/octet-stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	tmp := target + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target)
}

func registryGet(ctx context.Context, rawURL, accept string) ([]byte, error) {
	resp, err := registryDo(ctx, rawURL, accept)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func registryDo(ctx context.Context, rawURL, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		auth := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		token, err := registryBearerToken(ctx, auth)
		if err != nil {
			return nil, err
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	return resp, nil
}

func registryBearerToken(ctx context.Context, header string) (string, error) {
	header = strings.TrimSpace(strings.TrimPrefix(header, "Bearer"))
	parts := strings.Split(header, ",")
	values := map[string]string{}
	for _, part := range parts {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		values[k] = strings.Trim(v, `"`)
	}
	realm := values["realm"]
	if realm == "" {
		return "", fmt.Errorf("registry auth challenge missing realm")
	}
	u, err := url.Parse(realm)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for _, key := range []string{"service", "scope"} {
		if values[key] != "" {
			q.Set(key, values[key])
		}
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	if token := registryAuthToken(); token != "" {
		req.SetBasicAuth(firstNonEmpty(os.Getenv("GHCR_USER"), os.Getenv("GITHUB_ACTOR"), "oauth2"), token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("registry token request: %s", resp.Status)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", fmt.Errorf("registry token response missing token")
	}
	return out.Token, nil
}

func registryAuthToken() string {
	for _, key := range []string{"GHCR_TOKEN", "GITHUB_TOKEN", "CR_PAT"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func digestFile(digest string) string {
	return strings.ReplaceAll(digest, ":", "-")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
