package oci

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPinnedManifestRejectsSubstitutionWithoutDigestHeader(t *testing.T) {
	body := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		w.Write(body)
	}))
	defer server.Close()
	store := NewStore(t.TempDir())
	reg := &registryContext{registry: server.URL, client: server.Client()}
	if _, _, err := store.fetchManifest(t.Context(), reg, "image", "sha256:"+strings.Repeat("a", 64), "amd64"); err == nil {
		t.Fatal("accepted substituted manifest")
	}
	pin := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	if _, digest, err := store.fetchManifest(t.Context(), reg, "image", pin, "amd64"); err != nil || digest != pin {
		t.Fatalf("valid pin: %s %v", digest, err)
	}
}
func TestManifestIndexRejectsSubstitutedPlatformManifest(t *testing.T) {
	index := fmt.Sprintf(`{"schemaVersion":2,"manifests":[{"digest":"sha256:%s","platform":{"os":"linux","architecture":"amd64"}}]}`, strings.Repeat("a", 64))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tag") {
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			fmt.Fprint(w, index)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		fmt.Fprint(w, `{"schemaVersion":2,"layers":[]}`)
	}))
	defer server.Close()
	store := NewStore(t.TempDir())
	reg := &registryContext{registry: server.URL, client: server.Client()}
	if _, _, err := store.fetchManifest(t.Context(), reg, "image", "tag", "amd64"); err == nil {
		t.Fatal("accepted substituted platform manifest")
	}
}
