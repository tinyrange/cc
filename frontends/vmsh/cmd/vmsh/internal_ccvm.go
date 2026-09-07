package main

import (
	"os"

	"j5.nz/cc/frontends/vmsh/internal/backend"
	"j5.nz/cc/frontends/vmsh/internal/vmshd"
	"j5.nz/cc/frontends/vmsh/internal/vmshdprotocol"
)

func bundledCCVMAvailable() bool {
	return true
}

func runInternalCCVMFromEnv() bool {
	if os.Getenv(backend.InternalVMSHDEnv) == "1" || vmshdprotocol.IsDaemonExecutableName(os.Args[0]) {
		_ = os.Setenv(backend.InternalCCVMSidecarModeEnv, backend.InternalCCVMSidecarMode)
		vmshd.Main(os.Args[1:])
		return true
	}
	return false
}
