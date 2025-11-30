package virtio

// // ConsolePCITemplate creates a virtio-console device exposed via PCI.
// type ConsolePCITemplate struct {
// 	Host     *pcipkg.HostBridge
// 	Bus      uint8
// 	Device   uint8
// 	Function uint8

// 	Out io.Writer
// 	In  io.Reader
// }

// // Create implements hv.DeviceTemplate.
// func (t ConsolePCITemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
// 	if t.Host == nil {
// 		return nil, fmt.Errorf("virtio-console: PCI template requires a host bridge")
// 	}

// 	console := &Console{
// 		out: t.Out,
// 		in:  t.In,
// 	}

// 	bus := t.Bus
// 	dev := t.Device
// 	fn := t.Function
// 	if dev == 0 {
// 		dev = 1
// 	}
// 	device, err := newPCIDevice(vm, t.Host, bus, dev, fn, consoleDeviceID, consoleDeviceID, []uint64{virtioFeatureVersion1}, console)
// 	if err != nil {
// 		return nil, fmt.Errorf("virtio-console: create pci device: %w", err)
// 	}
// 	console.device = device

// 	if err := console.Init(vm); err != nil {
// 		return nil, fmt.Errorf("virtio-console: initialize pci device: %w", err)
// 	}
// 	return console, nil
// }

// var _ hv.DeviceTemplate = ConsolePCITemplate{}
