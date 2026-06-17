package builtin

import (
	"context"
	"os"
	"path/filepath"

	freebsdrootfs "j5.nz/cc/internal/freebsd/rootfs"
	managedguest "j5.nz/cc/internal/managed/guest"
	"j5.nz/cc/internal/managed/machine"
	"j5.nz/cc/internal/managed/rootartifact"
	netbsdrootfs "j5.nz/cc/internal/netbsd/rootfs"
	openbsdrootfs "j5.nz/cc/internal/openbsd/rootfs"
	"j5.nz/cc/internal/vmruntime"
)

type BSDDefinition struct {
	Profile       managedguest.Profile
	BootKind      string
	Hostname      string
	Interface     string
	CacheDir      string
	BuildArtifact func(context.Context, string, machine.NetworkSpec) (rootartifact.Artifact, error)
}

func GuestForImage(image string) (managedguest.Profile, bool) {
	return managedguest.BuiltinBSDProfileForImage(image)
}

func IsGuestImage(image string) bool {
	_, ok := GuestForImage(image)
	return ok
}

func OpenBSDDefinition(guestInitCache string) BSDDefinition {
	return OpenBSDDefinitionForArch(guestInitCache, "")
}

func OpenBSDDefinitionForArch(guestInitCache, arch string) BSDDefinition {
	return BSDDefinition{
		Profile:   managedguest.OpenBSDProfile,
		BootKind:  "openbsd",
		Hostname:  "cc-openbsd",
		Interface: "vio0",
		CacheDir:  OpenBSDRuntimeCacheDir(guestInitCache),
		BuildArtifact: func(ctx context.Context, cacheDir string, network machine.NetworkSpec) (rootartifact.Artifact, error) {
			runtime, err := openbsdrootfs.BuildManagedRuntime(ctx, openbsdrootfs.Config{CacheDir: cacheDir, Arch: arch, Network: network})
			if err != nil {
				return rootartifact.Artifact{}, err
			}
			return runtime.Artifact(), nil
		},
	}
}

func OpenBSDRuntimeConfig(guestInitCache string) openbsdrootfs.Config {
	return openbsdrootfs.Config{CacheDir: OpenBSDRuntimeCacheDir(guestInitCache)}
}

func OpenBSDRuntimeCacheDir(guestInitCache string) string {
	cacheDir := guestInitCache
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "cc-openbsd")
	} else {
		cacheDir = filepath.Join(filepath.Dir(cacheDir), "openbsd")
	}
	return cacheDir
}

func FreeBSDDefinition(guestInitCache string) BSDDefinition {
	return FreeBSDDefinitionForArch(guestInitCache, "")
}

func FreeBSDDefinitionForArch(guestInitCache, arch string) BSDDefinition {
	return BSDDefinition{
		Profile:   managedguest.FreeBSDProfile,
		BootKind:  "freebsd",
		Hostname:  "cc-freebsd",
		Interface: "vtnet0",
		CacheDir:  FreeBSDRuntimeCacheDir(guestInitCache),
		BuildArtifact: func(ctx context.Context, cacheDir string, network machine.NetworkSpec) (rootartifact.Artifact, error) {
			runtime, err := freebsdrootfs.BuildManagedRuntime(ctx, freebsdrootfs.Config{CacheDir: cacheDir, Arch: arch, Network: network})
			if err != nil {
				return rootartifact.Artifact{}, err
			}
			return runtime.Artifact(), nil
		},
	}
}

func FreeBSDRuntimeConfig(guestInitCache string) freebsdrootfs.Config {
	return freebsdrootfs.Config{CacheDir: FreeBSDRuntimeCacheDir(guestInitCache)}
}

func FreeBSDRuntimeCacheDir(guestInitCache string) string {
	cacheDir := guestInitCache
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "cc-freebsd")
	} else {
		cacheDir = filepath.Join(filepath.Dir(cacheDir), "freebsd")
	}
	return cacheDir
}

func NetBSDDefinition(guestInitCache string) BSDDefinition {
	return NetBSDDefinitionForArch(guestInitCache, "")
}

func NetBSDDefinitionForArch(guestInitCache, arch string) BSDDefinition {
	return BSDDefinition{
		Profile:   managedguest.NetBSDProfile,
		BootKind:  "netbsd",
		Hostname:  "cc-netbsd",
		Interface: "vioif0",
		CacheDir:  NetBSDRuntimeCacheDir(guestInitCache),
		BuildArtifact: func(ctx context.Context, cacheDir string, network machine.NetworkSpec) (rootartifact.Artifact, error) {
			runtime, err := netbsdrootfs.BuildManagedRuntime(ctx, netbsdrootfs.Config{CacheDir: cacheDir, Arch: arch, Network: network})
			if err != nil {
				return rootartifact.Artifact{}, err
			}
			return runtime.Artifact(), nil
		},
	}
}

func NetBSDRuntimeConfig(guestInitCache string) netbsdrootfs.Config {
	return netbsdrootfs.Config{CacheDir: NetBSDRuntimeCacheDir(guestInitCache)}
}

func NetBSDRuntimeCacheDir(guestInitCache string) string {
	cacheDir := guestInitCache
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "cc-netbsd")
	} else {
		cacheDir = filepath.Join(filepath.Dir(cacheDir), "netbsd")
	}
	return cacheDir
}

func EffectiveExecEnv(base, overrides []string, replace bool) []string {
	defaults := []string{
		"PATH=/bin:/sbin:/usr/bin:/usr/sbin",
		"HOME=/root",
		"TERM=xterm",
	}
	if replace {
		return vmruntime.MergeEnv(defaults, overrides)
	}
	return vmruntime.MergeEnv(vmruntime.MergeEnv(defaults, base), overrides)
}
