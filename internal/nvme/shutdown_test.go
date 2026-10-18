package nvme

import "testing"

func TestShutdownPreservesQueuesUntilDisable(t *testing.T) {
	c := NewController(newTestDisk(1 << 20))
	c.Attach(newTestMemory(0x10000), &testIRQ{})
	mustWriteMMIO(t, c, regAQA, 15|15<<16)
	mustWriteMMIO(t, c, regASQ, 0x1000)
	mustWriteMMIO(t, c, regACQ, 0x2000)
	mustWriteMMIO(t, c, regCC, 0x460001)
	q := c.queues[0]
	mustWriteMMIO(t, c, regCC, 0x464001)
	status, err := c.ReadMMIO(regCSTS, 4)
	if err != nil || status != 9 || c.queues[0] != q {
		t.Fatalf("shutdown status=%#x, err=%v", status, err)
	}
	mustWriteMMIO(t, c, regCC, 0)
	status, err = c.ReadMMIO(regCSTS, 4)
	if err != nil || status != 0 || len(c.queues) != 0 {
		t.Fatal("disable did not reset controller")
	}
}
