package oci

import "testing"

func TestParseImageRef(t *testing.T) {
	tests := []struct {
		name         string
		imageRef     string
		wantRegistry string
		wantImage    string
		wantTag      string
		wantErr      bool
	}{
		{
			name:         "simple alpine",
			imageRef:     "alpine",
			wantRegistry: defaultRegistry,
			wantImage:    "library/alpine",
			wantTag:      "latest",
		},
		{
			name:         "alpine with tag",
			imageRef:     "alpine:3.19",
			wantRegistry: defaultRegistry,
			wantImage:    "library/alpine",
			wantTag:      "3.19",
		},
		{
			name:         "user image with underscore and dot in name",
			imageRef:     "jerync/oshyx_0.4:20220614",
			wantRegistry: defaultRegistry,
			wantImage:    "jerync/oshyx_0.4",
			wantTag:      "20220614",
		},
		{
			name:         "user image with dot in name no tag",
			imageRef:     "jerync/oshyx_0.4",
			wantRegistry: defaultRegistry,
			wantImage:    "jerync/oshyx_0.4",
			wantTag:      "latest",
		},
		{
			name:         "gcr.io registry",
			imageRef:     "gcr.io/project/image:v1",
			wantRegistry: "https://gcr.io/v2",
			wantImage:    "project/image",
			wantTag:      "v1",
		},
		{
			name:         "localhost registry",
			imageRef:     "localhost:5000/image:test",
			wantRegistry: "https://localhost:5000/v2",
			wantImage:    "image",
			wantTag:      "test",
		},
		{
			name:         "localhost registry no port",
			imageRef:     "localhost/myimage:v1",
			wantRegistry: "https://localhost/v2",
			wantImage:    "myimage",
			wantTag:      "v1",
		},
		{
			name:         "docker.io explicit",
			imageRef:     "docker.io/library/nginx:latest",
			wantRegistry: defaultRegistry,
			wantImage:    "library/nginx",
			wantTag:      "latest",
		},
		{
			name:         "quay.io registry",
			imageRef:     "quay.io/prometheus/prometheus:v2.45.0",
			wantRegistry: "https://quay.io/v2",
			wantImage:    "prometheus/prometheus",
			wantTag:      "v2.45.0",
		},
		{
			name:         "nested path on custom registry",
			imageRef:     "my.registry.com/org/project/image:tag",
			wantRegistry: "https://my.registry.com/v2",
			wantImage:    "org/project/image",
			wantTag:      "tag",
		},
		{
			name:         "user/repo style",
			imageRef:     "myuser/myrepo",
			wantRegistry: defaultRegistry,
			wantImage:    "myuser/myrepo",
			wantTag:      "latest",
		},
		{
			name:         "image with multiple dots in later components",
			imageRef:     "myuser/my.repo.name:v1.2.3",
			wantRegistry: defaultRegistry,
			wantImage:    "myuser/my.repo.name",
			wantTag:      "v1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRegistry, gotImage, gotTag, err := ParseImageRef(tt.imageRef)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseImageRef() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotRegistry != tt.wantRegistry {
				t.Errorf("ParseImageRef() registry = %v, want %v", gotRegistry, tt.wantRegistry)
			}
			if gotImage != tt.wantImage {
				t.Errorf("ParseImageRef() image = %v, want %v", gotImage, tt.wantImage)
			}
			if gotTag != tt.wantTag {
				t.Errorf("ParseImageRef() tag = %v, want %v", gotTag, tt.wantTag)
			}
		})
	}
}
