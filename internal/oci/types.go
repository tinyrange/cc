package oci

// RuntimeConfig holds the runtime configuration extracted from an OCI image.
type RuntimeConfig struct {
	Layers     []string          `json:"layers"`
	Env        []string          `json:"env,omitempty"`
	Entrypoint []string          `json:"entrypoint,omitempty"`
	Cmd        []string          `json:"cmd,omitempty"`
	WorkingDir string            `json:"workingDir,omitempty"`
	User       string            `json:"user,omitempty"`
	UID        *int              `json:"uid,omitempty"`
	GID        *int              `json:"gid,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// ImageLayer represents a single layer in an OCI image.
type ImageLayer struct {
	Hash         string // sha256:... digest
	IndexPath    string // path to .idx file
	ContentsPath string // path to .contents file
}

// Image represents a pulled OCI image ready for use.
type Image struct {
	Config RuntimeConfig
	Layers []ImageLayer
	Dir    string // directory containing the image files
}

// Command returns the command to run, combining entrypoint and cmd.
// If overrideCmd is provided, it replaces the cmd portion.
func (img *Image) Command(overrideCmd []string) []string {
	if len(overrideCmd) > 0 {
		if len(img.Config.Entrypoint) > 0 {
			return append(img.Config.Entrypoint, overrideCmd...)
		}
		return overrideCmd
	}
	if len(img.Config.Entrypoint) > 0 && len(img.Config.Cmd) > 0 {
		return append(img.Config.Entrypoint, img.Config.Cmd...)
	}
	if len(img.Config.Entrypoint) > 0 {
		return img.Config.Entrypoint
	}
	return img.Config.Cmd
}
