package guest

const (
	OpenBSDImageName = "@openbsd"
	FreeBSDImageName = "@freebsd"
	NetBSDImageName  = "@netbsd"
)

var OpenBSDProfile = Profile{
	Name:      "OpenBSD",
	Canonical: OpenBSDImageName,
	Aliases:   []string{"openbsd"},
	Caps: Capabilities{
		PersistentExec:    true,
		StreamingExec:     true,
		Signals:           true,
		CopyIn:            true,
		CopyOut:           true,
		ArchiveExtract:    true,
		Network:           true,
		DNS:               true,
		PackageManager:    "pkg_add",
		RootSnapshot:      true,
		WritableRootBlock: true,
	},
}

var FreeBSDProfile = Profile{
	Name:      "FreeBSD",
	Canonical: FreeBSDImageName,
	Aliases:   []string{"freebsd"},
	Caps: Capabilities{
		PersistentExec:    true,
		StreamingExec:     true,
		TTY:               true,
		ResizeTTY:         true,
		Signals:           true,
		CopyIn:            true,
		CopyOut:           true,
		ArchiveExtract:    true,
		Network:           true,
		DNS:               true,
		PackageManager:    "pkg",
		RootSnapshot:      true,
		WritableRootBlock: true,
	},
}

var NetBSDProfile = Profile{
	Name:      "NetBSD",
	Canonical: NetBSDImageName,
	Aliases:   []string{"netbsd"},
	Caps: Capabilities{
		PersistentExec:    true,
		StreamingExec:     true,
		Signals:           true,
		CopyIn:            true,
		CopyOut:           true,
		ArchiveExtract:    true,
		Network:           true,
		DNS:               true,
		PackageManager:    "pkg_add",
		RootSnapshot:      true,
		WritableRootBlock: true,
	},
}

var BSDProfiles = []Profile{
	OpenBSDProfile,
	FreeBSDProfile,
	NetBSDProfile,
}

func BuiltinBSDProfileForImage(image string) (Profile, bool) {
	for _, profile := range BSDProfiles {
		if profile.Match(image) {
			return profile, true
		}
	}
	return Profile{}, false
}

func IsBuiltinBSDImage(image string) bool {
	_, ok := BuiltinBSDProfileForImage(image)
	return ok
}
