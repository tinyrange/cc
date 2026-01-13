package main

import (
	"testing"
)

func TestParseCrumbleCrackerURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantErr  bool
		action   string
		imageRef string
	}{
		{
			name:     "simple alpine",
			url:      "crumblecracker://run/alpine",
			wantErr:  false,
			action:   "run",
			imageRef: "alpine",
		},
		{
			name:     "alpine with tag",
			url:      "crumblecracker://run/alpine:latest",
			wantErr:  false,
			action:   "run",
			imageRef: "alpine:latest",
		},
		{
			name:     "nginx with version",
			url:      "crumblecracker://run/nginx:1.25",
			wantErr:  false,
			action:   "run",
			imageRef: "nginx:1.25",
		},
		{
			name:     "ghcr.io image",
			url:      "crumblecracker://run/ghcr.io/user/image:tag",
			wantErr:  false,
			action:   "run",
			imageRef: "ghcr.io/user/image:tag",
		},
		{
			name:     "docker hub namespaced",
			url:      "crumblecracker://run/library/ubuntu:22.04",
			wantErr:  false,
			action:   "run",
			imageRef: "library/ubuntu:22.04",
		},
		{
			name:    "wrong scheme",
			url:     "http://run/alpine",
			wantErr: true,
		},
		{
			name:    "missing action",
			url:     "crumblecracker:///alpine",
			wantErr: true,
		},
		{
			name:    "missing image ref",
			url:     "crumblecracker://run/",
			wantErr: true,
		},
		{
			name:    "missing image ref no trailing slash",
			url:     "crumblecracker://run",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, err := ParseCrumbleCrackerURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCrumbleCrackerURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if action.Action != tt.action {
				t.Errorf("action = %q, want %q", action.Action, tt.action)
			}
			if action.ImageRef != tt.imageRef {
				t.Errorf("imageRef = %q, want %q", action.ImageRef, tt.imageRef)
			}
		})
	}
}

func TestValidateURLAction(t *testing.T) {
	tests := []struct {
		name    string
		action  *URLAction
		wantErr bool
	}{
		{
			name:    "valid run action",
			action:  &URLAction{Action: "run", ImageRef: "alpine"},
			wantErr: false,
		},
		{
			name:    "unknown action",
			action:  &URLAction{Action: "unknown", ImageRef: "alpine"},
			wantErr: true,
		},
		{
			name:    "nil action",
			action:  nil,
			wantErr: true,
		},
		{
			name:    "empty image ref",
			action:  &URLAction{Action: "run", ImageRef: ""},
			wantErr: true,
		},
		{
			name:    "image ref with whitespace",
			action:  &URLAction{Action: "run", ImageRef: "alpine latest"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURLAction(tt.action)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURLAction() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSanitizeImageNameForDisplay(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "alpine",
			expected: "alpine",
		},
		{
			input:    "very-long-image-name-that-exceeds-the-maximum-length-limit-for-display-purposes-and-should-be-truncated",
			expected: "very-long-image-name-that-exceeds-the-maximum-length-limit-for-display-purpos...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SanitizeImageNameForDisplay(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeImageNameForDisplay(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
