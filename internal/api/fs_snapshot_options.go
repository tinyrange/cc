package api

// fsSnapshotConfig holds configuration for filesystem snapshot operations.
type fsSnapshotConfig struct {
	excludes    []string
	cachePrefix string
	cacheDir    string
}

// fsSnapshotOption implements FilesystemSnapshotOption.
type fsSnapshotOption struct {
	apply func(*fsSnapshotConfig)
}

func (o *fsSnapshotOption) IsFilesystemSnapshotOption() {}

// WithExcludes specifies path patterns to exclude from the snapshot.
// Patterns use glob-style matching (*, ?, []).
func WithExcludes(patterns ...string) FilesystemSnapshotOption {
	return &fsSnapshotOption{
		apply: func(c *fsSnapshotConfig) {
			c.excludes = append(c.excludes, patterns...)
		},
	}
}

// WithCachePrefix specifies a prefix for the cache key.
// This can be used to namespace snapshots.
func WithCachePrefix(prefix string) FilesystemSnapshotOption {
	return &fsSnapshotOption{
		apply: func(c *fsSnapshotConfig) {
			c.cachePrefix = prefix
		},
	}
}

// WithCacheDir specifies the directory where snapshot layers are stored.
func WithCacheDir(dir string) FilesystemSnapshotOption {
	return &fsSnapshotOption{
		apply: func(c *fsSnapshotConfig) {
			c.cacheDir = dir
		},
	}
}

// parseFSSnapshotOptions parses snapshot options into a config.
func parseFSSnapshotOptions(opts []FilesystemSnapshotOption) *fsSnapshotConfig {
	cfg := &fsSnapshotConfig{}
	for _, opt := range opts {
		if o, ok := opt.(*fsSnapshotOption); ok {
			o.apply(cfg)
		}
	}
	return cfg
}
