//go:build linux && amd64

package kvm

func (h *hypervisor) supportedMSRs() ([]uint32, error) {
	h.supportedMsrsOnce.Do(func() {
		list, err := getMsrIndexList(h.fd)
		if err != nil {
			h.supportedMsrsErr = err
			return
		}

		h.supportedMsrs = list
	})

	return h.supportedMsrs, h.supportedMsrsErr
}

const (
	msrIA32TSC         = 0x00000010
	msrIA32SysenterCS  = 0x00000174
	msrIA32SysenterESP = 0x00000175
	msrIA32SysenterEIP = 0x00000176
	msrIA32PAT         = 0x00000277
	msrStar            = 0xc0000081
	msrLStar           = 0xc0000082
	msrCStar           = 0xc0000083
	msrSyscallMask     = 0xc0000084
	msrFsBase          = 0xc0000100
	msrGsBase          = 0xc0000101
	msrKernelGsBase    = 0xc0000102
	msrTscAux          = 0xc0000103
)

var snapshotMsrWhitelist = []uint32{
	msrIA32TSC,
	msrIA32SysenterCS,
	msrIA32SysenterESP,
	msrIA32SysenterEIP,
	msrIA32PAT,
	msrStar,
	msrLStar,
	msrCStar,
	msrSyscallMask,
	msrFsBase,
	msrGsBase,
	msrKernelGsBase,
	msrTscAux,
}

func (h *hypervisor) snapshotMSRs() ([]uint32, error) {
	h.snapshotMsrsOnce.Do(func() {
		supported, err := h.supportedMSRs()
		if err != nil {
			h.snapshotMsrsErr = err
			return
		}

		supportedSet := make(map[uint32]struct{}, len(supported))
		for _, idx := range supported {
			supportedSet[idx] = struct{}{}
		}

		var filtered []uint32
		for _, idx := range snapshotMsrWhitelist {
			if _, ok := supportedSet[idx]; ok {
				filtered = append(filtered, idx)
			}
		}

		h.snapshotMsrs = filtered
	})

	return h.snapshotMsrs, h.snapshotMsrsErr
}
