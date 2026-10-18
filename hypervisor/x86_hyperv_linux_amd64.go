//go:build linux && amd64

package hypervisor

import "j5.nz/cc/hypervisor/x86state"

func (n *nativeX86) SupportedHyperVCPUID() ([]x86state.CPUIDEntry, error) {
	return n.vm.SupportedHyperVCPUID()
}
func (c *nativeX86CPU) SupportedHyperVCPUID() ([]x86state.CPUIDEntry, error) {
	return c.owner.SupportedHyperVCPUID()
}

func (n *nativeX86) enableHyperVSynIC(index int) (enabled bool, err error) {
	err = n.threads[index].call(func() error { var e error; enabled, e = n.vm.EnableHyperVSynIC(index); return e })
	return
}
func (n *nativeX86) EnableHyperVSynIC() (bool, error)    { return n.enableHyperVSynIC(0) }
func (c *nativeX86CPU) EnableHyperVSynIC() (bool, error) { return c.owner.enableHyperVSynIC(c.index) }
