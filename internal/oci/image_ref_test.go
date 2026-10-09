package oci

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"j5.nz/cc/internal/download"
)

const acceptanceAlpineDigest = "sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8"

func TestParseImageRefDigestPrecedesTag(t *testing.T) {
	for _, tc := range []struct{ reference, registry, image, selector string }{
		{"docker.io/library/alpine@" + acceptanceAlpineDigest, defaultRegistry, "library/alpine", acceptanceAlpineDigest},
		{"docker.io/library/alpine:3.22@" + acceptanceAlpineDigest, defaultRegistry, "library/alpine", acceptanceAlpineDigest},
		{"alpine@" + acceptanceAlpineDigest, defaultRegistry, "library/alpine", acceptanceAlpineDigest},
		{"alpine:3.22@" + acceptanceAlpineDigest, defaultRegistry, "library/alpine", acceptanceAlpineDigest},
		{"localhost:5000/team/alpine:3.22@" + acceptanceAlpineDigest, "https://localhost:5000/v2", "team/alpine", acceptanceAlpineDigest},
		{"localhost:5000/team/alpine@" + acceptanceAlpineDigest, "https://localhost:5000/v2", "team/alpine", acceptanceAlpineDigest},
		{"localhost:5000/team/alpine:3.22", "https://localhost:5000/v2", "team/alpine", "3.22"},
		{"alpine:3.22", defaultRegistry, "library/alpine", "3.22"},
		{"alpine", defaultRegistry, "library/alpine", "latest"},
	} {
		t.Run(tc.reference, func(t *testing.T) {
			registry, image, selector, err := ParseImageRef(tc.reference)
			if err != nil {
				t.Fatal(err)
			}
			if registry != tc.registry || image != tc.image || selector != tc.selector {
				t.Fatalf("parsed (%q,%q,%q), want (%q,%q,%q)", registry, image, selector, tc.registry, tc.image, tc.selector)
			}
		})
	}
	for _, reference := range []string{"alpine@", "@" + acceptanceAlpineDigest, "alpine@" + acceptanceAlpineDigest + "@other"} {
		if _, _, _, err := ParseImageRef(reference); err == nil {
			t.Fatalf("accepted malformed digest reference %q", reference)
		}
	}
}

func TestParsedImageRefReachesManifestDigestVerification(t *testing.T) {
	body := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[]}`)
	validDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	for _, tc := range []struct {
		reference, pin string
		substituted    bool
	}{
		{"docker.io/library/alpine@" + acceptanceAlpineDigest, acceptanceAlpineDigest, true},
		{"docker.io/library/alpine:3.22@" + acceptanceAlpineDigest, acceptanceAlpineDigest, true},
		{"docker.io/library/alpine@" + validDigest, validDigest, false},
		{"docker.io/library/alpine:3.22@" + validDigest, validDigest, false},
	} {
		t.Run(tc.reference, func(t *testing.T) {
			_, image, selector, err := ParseImageRef(tc.reference)
			if err != nil {
				t.Fatal(err)
			}
			requested := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requested <- r.URL.Path
				// No Docker-Content-Digest header: verification must use the parsed pin.
				w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
				_, _ = w.Write(body)
			}))
			defer server.Close()
			store := NewStore(t.TempDir())
			reg := &registryContext{registry: server.URL + "/v2", client: server.Client()}
			_, digest, err := store.fetchManifest(t.Context(), reg, image, selector, "amd64")
			gotPath := <-requested
			wantPath := "/v2/library/alpine/manifests/" + tc.pin
			if gotPath != wantPath {
				t.Fatalf("requested %q, want %q", gotPath, wantPath)
			}
			if strings.Contains(image, "@") || strings.Contains(image, ":") {
				t.Fatalf("selector leaked into repository: %q", image)
			}
			if tc.substituted {
				var mismatch *download.DigestError
				if !errors.As(err, &mismatch) {
					t.Fatalf("substitution error = %v, want digest mismatch", err)
				}
				if mismatch.Expected != tc.pin || mismatch.Actual != validDigest {
					t.Fatalf("wrong digest verification: %+v", mismatch)
				}
			} else if err != nil || digest != validDigest {
				t.Fatalf("valid pinned manifest: digest=%q err=%v", digest, err)
			}
		})
	}
}
