//go:build windows && amd64

package whp

import (
	"encoding/binary"
	"testing"
)

func TestBootIOAPICEdgeTriggeredInterrupt(t *testing.T) {
	var ioapic bootIOAPIC
	ioapic.init()
	programBootIOAPICRedirection(t, &ioapic, 0, 0x30, 1, false, false)

	route, ok := ioapic.assert(0, true)
	if !ok {
		t.Fatal("assert did not produce an interrupt route")
	}
	if route.line != 0 || route.vector != 0x30 || route.level {
		t.Fatalf("route = %+v, want edge vector 0x30 on line 0", route)
	}

	if _, ok := ioapic.assert(0, true); ok {
		t.Fatal("second assert without deassert produced another edge interrupt")
	}

	ioapic.assert(0, false)
	if route, ok := ioapic.assert(0, true); !ok || route.vector != 0x30 || route.level {
		t.Fatalf("assert after deassert = %+v, ok=%t, want edge vector 0x30", route, ok)
	}
}

func TestBootIOAPICUnmaskReplaysAssertedEdge(t *testing.T) {
	var ioapic bootIOAPIC
	ioapic.init()
	programBootIOAPICRedirection(t, &ioapic, 2, 0x55, 2, false, true)

	if _, ok := ioapic.assert(2, true); ok {
		t.Fatal("masked line produced an interrupt route")
	}
	route, ok := programBootIOAPICRedirection(t, &ioapic, 2, 0x55, 2, false, false)
	if !ok {
		t.Fatal("unmasking asserted edge line did not produce an interrupt route")
	}
	if route.line != 2 || route.vector != 0x55 || route.level {
		t.Fatalf("route = %+v, want edge vector 0x55 on line 2", route)
	}
}

func TestBootIOAPICDeviceHighRouteCanBeReasserted(t *testing.T) {
	var ioapic bootIOAPIC
	ioapic.init()
	programBootIOAPICRedirection(t, &ioapic, 6, 0x42, 0, false, false)

	if _, ok := ioapic.deviceHighRoute(6); ok {
		t.Fatal("inactive edge line produced a reassertion route")
	}
	if route, ok := ioapic.assert(6, true); !ok || route.vector != 0x42 || route.level {
		t.Fatalf("initial edge assert = %+v, ok=%t, want vector 0x42", route, ok)
	}
	if route, ok := ioapic.deviceHighRoute(6); !ok || route.vector != 0x42 || route.level {
		t.Fatalf("edge reassert route = %+v, ok=%t, want vector 0x42", route, ok)
	}
	ioapic.assert(6, false)
	if _, ok := ioapic.deviceHighRoute(6); ok {
		t.Fatal("deasserted edge line produced a reassertion route")
	}
}

func TestBootIOAPICDeviceHighRouteCanReissueLevel(t *testing.T) {
	var ioapic bootIOAPIC
	ioapic.init()
	programBootIOAPICRedirection(t, &ioapic, 6, 0x42, 0, true, false)

	if route, ok := ioapic.assert(6, true); !ok || route.vector != 0x42 || !route.level {
		t.Fatalf("initial level assert = %+v, ok=%t, want level vector 0x42", route, ok)
	}
	if route, ok := ioapic.deviceHighRoute(6); !ok || route.vector != 0x42 || !route.level {
		t.Fatalf("level reissue route = %+v, ok=%t, want level vector 0x42", route, ok)
	}
}

func TestBootIOAPICLevelReassertAfterEOI(t *testing.T) {
	var ioapic bootIOAPIC
	ioapic.init()
	programBootIOAPICRedirection(t, &ioapic, 1, 0x41, 0, true, false)

	route, ok := ioapic.assert(1, true)
	if !ok {
		t.Fatal("level assert did not produce an interrupt route")
	}
	if !route.level {
		t.Fatalf("route = %+v, want level-triggered route", route)
	}
	if bootIOAPICInFlight(&ioapic, 0x41) {
		t.Fatal("level assert marked interrupt in-flight before delivery")
	}
	if !ioapic.beginInterrupt(0x41) {
		t.Fatal("beginInterrupt rejected newly asserted level interrupt")
	}
	if _, ok := ioapic.assert(1, true); ok {
		t.Fatal("level assert while in-flight produced a duplicate route")
	}

	route, ok = ioapic.handleEOI(0x41)
	if !ok {
		t.Fatal("EOI did not reassert still-high level line")
	}
	if route.vector != 0x41 || !route.level {
		t.Fatalf("EOI route = %+v, want level vector 0x41", route)
	}

	ioapic.assert(1, false)
	if bootIOAPICInFlight(&ioapic, 0x41) {
		t.Fatal("in-flight state not cleared after deassert")
	}
}

func TestCanAcceptInterruptRequiresOpenGate(t *testing.T) {
	var ctx runVPExitContext
	ctx.VpContext.Rflags = 1 << 9

	if !canAcceptInterrupt(&ctx, 0x22) {
		t.Fatal("open interrupt gate rejected vector")
	}
	ctx.VpContext.ExecutionState.AsUint16 = 1 << 6
	if canAcceptInterrupt(&ctx, 0x22) {
		t.Fatal("interruption-pending gate accepted vector")
	}
	ctx.VpContext.ExecutionState.AsUint16 = 1 << 12
	if canAcceptInterrupt(&ctx, 0x22) {
		t.Fatal("interrupt-shadow gate accepted vector")
	}
	ctx.VpContext.ExecutionState.AsUint16 = 0
	ctx.VpContext.Rflags = 0
	if canAcceptInterrupt(&ctx, 0x22) {
		t.Fatal("disabled IF gate accepted vector")
	}
}

func TestCanAcceptInterruptHonorsCR8Priority(t *testing.T) {
	var ctx runVPExitContext
	ctx.VpContext.Rflags = 1 << 9
	ctx.VpContext.InstructionLengthCr8 = 2 << 4

	if canAcceptInterrupt(&ctx, 0x22) {
		t.Fatal("same-priority vector passed CR8 gate")
	}
	if !canAcceptInterrupt(&ctx, 0x32) {
		t.Fatal("higher-priority vector rejected by CR8 gate")
	}
}

func TestBootPITRearmStopsPreviousTicker(t *testing.T) {
	pit := newBootPIT(func() {})
	pit.mu.Lock()
	pit.channels[0].reload = 0xffff
	pit.armChannel0Locked()
	firstStop := pit.tickerStop
	pit.channels[0].reload = 0xfffe
	pit.armChannel0Locked()
	secondStop := pit.tickerStop
	pit.mu.Unlock()
	defer pit.Close()

	if firstStop == nil || secondStop == nil {
		t.Fatal("PIT did not create ticker stop channels")
	}
	if firstStop == secondStop {
		t.Fatal("PIT reused ticker stop channel after rearm")
	}
	select {
	case <-firstStop:
	default:
		t.Fatal("previous PIT ticker goroutine was not stopped on rearm")
	}
	select {
	case <-secondStop:
		t.Fatal("current PIT ticker was stopped before Close")
	default:
	}

	pit.Close()
	select {
	case <-secondStop:
	default:
		t.Fatal("current PIT ticker goroutine was not stopped on Close")
	}
}

func programBootIOAPICRedirection(t *testing.T, ioapic *bootIOAPIC, line uint8, vector uint8, dest uint8, level bool, masked bool) (bootIOAPICRoute, bool) {
	t.Helper()
	low := uint32(vector)
	if level {
		low |= 1 << 15
	}
	if masked {
		low |= 1 << 16
	}
	high := uint32(dest) << 24
	lowRoute, lowPending := writeBootIOAPICRegister(t, ioapic, uint32(0x10+line*2), low)
	highRoute, highPending := writeBootIOAPICRegister(t, ioapic, uint32(0x10+line*2+1), high)
	if highPending {
		return highRoute, true
	}
	return lowRoute, lowPending
}

func writeBootIOAPICRegister(t *testing.T, ioapic *bootIOAPIC, index uint32, value uint32) (bootIOAPICRoute, bool) {
	t.Helper()
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], index)
	handled, _, pending := ioapic.Write(ioapicBaseAddress, buf[:])
	if !handled || pending {
		t.Fatalf("write IOAPIC index handled=%t pending=%t", handled, pending)
	}
	binary.LittleEndian.PutUint32(buf[:], value)
	handled, route, pending := ioapic.Write(ioapicBaseAddress+0x10, buf[:])
	if !handled {
		t.Fatal("write IOAPIC data was not handled")
	}
	return route, pending
}

func bootIOAPICInFlight(ioapic *bootIOAPIC, vector uint8) bool {
	ioapic.mu.Lock()
	defer ioapic.mu.Unlock()
	return ioapic.inFlight[vector]
}
