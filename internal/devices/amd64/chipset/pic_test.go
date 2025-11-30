package chipset

import "testing"

type testReadySink struct {
	level bool
}

func (s *testReadySink) SetLevel(level bool) {
	s.level = level
}

func TestDualPICInitialization(t *testing.T) {
	sink := &testReadySink{}
	pic := NewDualPIC()
	pic.SetReadySink(sink)
	programPIC(t, pic)

	if pic.pics[0].initStage != initInitialized {
		t.Fatalf("primary PIC not initialized, stage=%v", pic.pics[0].initStage)
	}
	if pic.pics[1].initStage != initInitialized {
		t.Fatalf("secondary PIC not initialized, stage=%v", pic.pics[1].initStage)
	}
	if sink.level {
		t.Fatalf("ready line unexpectedly high after initialization")
	}
}

func TestDualPICEdgeInterruptPrimary(t *testing.T) {
	pic, sink := initializedPIC(t)
	const irqLine = 0

	pic.SetIRQ(irqLine, true)
	if !sink.level {
		t.Fatalf("ready line not asserted for primary IRQ")
	}

	requested, vec := pic.Acknowledge()
	if !requested {
		t.Fatalf("expected interrupt to be acknowledged")
	}
	if vec != 0x30+irqLine {
		t.Fatalf("unexpected vector 0x%x", vec)
	}

	pic.SetIRQ(irqLine, false)
	sendEOI(t, pic, irqLine)
}

func TestDualPICEdgeInterruptSecondary(t *testing.T) {
	pic, sink := initializedPIC(t)
	const irqLine = 10 // maps to secondary line 2

	pic.SetIRQ(irqLine, true)
	if !sink.level {
		t.Fatalf("ready line not asserted for secondary IRQ")
	}

	requested, vec := pic.Acknowledge()
	if !requested {
		t.Fatalf("expected interrupt to be acknowledged")
	}
	if vec != 0x30+irqLine {
		t.Fatalf("unexpected vector 0x%x", vec)
	}

	pic.SetIRQ(irqLine, false)
	sendEOI(t, pic, irqLine)
}

func initializedPIC(t *testing.T) (*DualPIC, *testReadySink) {
	sink := &testReadySink{}
	pic := NewDualPIC()
	pic.SetReadySink(sink)
	programPIC(t, pic)
	return pic, sink
}

func programPIC(t *testing.T, pic *DualPIC) {
	writes := []struct {
		port uint16
		data byte
	}{
		{primaryPicCommandPort, 0x11},
		{primaryPicDataPort, 0x30},
		{primaryPicDataPort, 0x04},
		{primaryPicDataPort, 0x01},
		{secondaryPicCommandPort, 0x11},
		{secondaryPicDataPort, 0x38},
		{secondaryPicDataPort, 0x02},
		{secondaryPicDataPort, 0x01},
	}
	for _, w := range writes {
		if err := pic.WriteIOPort(w.port, []byte{w.data}); err != nil {
			t.Fatalf("write to 0x%x failed: %v", w.port, err)
		}
	}
}

func sendEOI(t *testing.T, pic *DualPIC, irq uint8) {
	var seq []struct {
		port  uint16
		value byte
	}
	if irq < 8 {
		seq = []struct {
			port  uint16
			value byte
		}{{
			primaryPicCommandPort,
			byte(0x60 | (irq & picIRQMask)),
		}}
	} else {
		seq = []struct {
			port  uint16
			value byte
		}{
			{
				primaryPicCommandPort,
				byte(0x60 | picChainCommunicationIRQ),
			},
			{
				secondaryPicCommandPort,
				byte(0x60 | ((irq - 8) & picIRQMask)),
			},
		}
	}
	for _, w := range seq {
		if err := pic.WriteIOPort(w.port, []byte{w.value}); err != nil {
			t.Fatalf("EOI write to 0x%x failed: %v", w.port, err)
		}
	}
}
