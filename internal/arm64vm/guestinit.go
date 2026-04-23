package arm64vm

import (
	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/vmruntime"
)

type GuestInitConfig = vmruntime.GuestInitConfig

type GuestInitShare = vmruntime.GuestInitShare

func MergeEnv(base, overrides []string) []string {
	return vmruntime.MergeEnv(base, overrides)
}

func HasEnvKey(env []string, key string) bool {
	return vmruntime.HasEnvKey(env, key)
}

func WithDefaultEnv(env []string) []string {
	return vmruntime.WithDefaultEnv(env)
}

func ModulePaths(modules []alpine.Module) []string {
	return vmruntime.ModulePaths(modules)
}

func EmulatorTagForPath(path string) string {
	return vmruntime.EmulatorTagForPath(path)
}

func GuestShareConfigs(shares []DirectoryShare) []GuestInitShare {
	return vmruntime.GuestShareConfigs(shares)
}

func BuildInitramfs(initPayload []byte, modules []alpine.Module, config GuestInitConfig) ([]byte, error) {
	return vmruntime.BuildInitramfs(initPayload, modules, config)
}
