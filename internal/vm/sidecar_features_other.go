//go:build !darwin || !arm64

package vm

type sidecarFeatures struct {
	supportsFSRPC bool
	supportsL2    bool
}

func sidecarHostFeatures() sidecarFeatures {
	return sidecarFeatures{}
}
