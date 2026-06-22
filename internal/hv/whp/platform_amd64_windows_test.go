//go:build windows && amd64

package whp

import "testing"

func TestBootPICLevelLineResamplesUntilDeasserted(t *testing.T) {
	var pic bootPIC
	pic.master.vectorBase = 0x20
	pic.slave.vectorBase = 0x28
	pic.master.mask = 0xff
	pic.slave.mask = 0xff
	pic.master.mask &^= 1 << 2
	pic.slave.mask &^= 1 << 2
	pic.elcr[1] = 1 << 2

	pic.SetIRQ(10, true)
	vector, line, ok := pic.AcknowledgePending()
	if !ok || vector != 0x2a || line != 10 {
		t.Fatalf("first ack = vector %#x line %d ok %t, want vector 0x2a line 10", vector, line, ok)
	}
	vector, line, ok = pic.AcknowledgePending()
	if !ok || vector != 0x2a || line != 10 {
		t.Fatalf("level ack = vector %#x line %d ok %t, want vector 0x2a line 10", vector, line, ok)
	}
	pic.SetIRQ(10, false)
	if vector, line, ok := pic.AcknowledgePending(); ok {
		t.Fatalf("ack after deassert = vector %#x line %d ok %t, want no pending irq", vector, line, ok)
	}
}

func TestBootPICEdgeLineRequiresNewRisingEdge(t *testing.T) {
	var pic bootPIC
	pic.master.vectorBase = 0x20
	pic.master.mask = 0xfe

	pic.SetIRQ(0, true)
	vector, line, ok := pic.AcknowledgePending()
	if !ok || vector != 0x20 || line != 0 {
		t.Fatalf("first ack = vector %#x line %d ok %t, want vector 0x20 line 0", vector, line, ok)
	}
	if vector, line, ok := pic.AcknowledgePending(); ok {
		t.Fatalf("second ack = vector %#x line %d ok %t, want no pending irq", vector, line, ok)
	}
	pic.SetIRQ(0, false)
	pic.SetIRQ(0, true)
	vector, line, ok = pic.AcknowledgePending()
	if !ok || vector != 0x20 || line != 0 {
		t.Fatalf("new edge ack = vector %#x line %d ok %t, want vector 0x20 line 0", vector, line, ok)
	}
}

func TestBootIOAPICActiveLowLevelLineUsesAssertedState(t *testing.T) {
	var ioapic bootIOAPIC
	ioapic.init()
	ioapic.redir[12] = 0x62 | 1<<13 | 1<<15

	route, pending := ioapic.assert(12, true)
	if !pending {
		t.Fatalf("assert active-low level line did not produce a pending route")
	}
	if route.line != 12 || route.vector != 0x62 || !route.level {
		t.Fatalf("route = %+v, want line 12 vector 0x62 level", route)
	}
	if _, pending := ioapic.deviceHighRoute(12); !pending {
		t.Fatalf("deviceHighRoute after assert = false, want true")
	}

	ioapic.assert(12, false)
	if _, pending := ioapic.deviceHighRoute(12); pending {
		t.Fatalf("deviceHighRoute after deassert = true, want false")
	}
}
