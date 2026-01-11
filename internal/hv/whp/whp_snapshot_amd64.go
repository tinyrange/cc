//go:build windows && amd64

package whp

// captureArchSnapshot captures AMD64-specific state.
// Currently no additional state is captured beyond registers.
func (v *virtualMachine) captureArchSnapshot(snap *whpSnapshot) error {
	return nil
}

// restoreArchSnapshot restores AMD64-specific state.
// Currently no additional state is restored beyond registers.
func (v *virtualMachine) restoreArchSnapshot(snap *whpSnapshot) error {
	return nil
}

// arm64GICSnapshot is a placeholder for cross-compilation compatibility.
// On AMD64, this is always nil.
type arm64GICSnapshot struct{}
