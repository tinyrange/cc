package nvme

// IOStatistics is a consistent snapshot of completed read/write commands.
// Counts describe controller commands, not backend physical storage operations.
type IOStatistics struct{ ReadCommands, ReadBytes, WriteCommands, WriteBytes uint64 }

func (c *Controller) IOStatistics() IOStatistics {
	c.mu.Lock()
	defer c.mu.Unlock()
	return IOStatistics{c.readOps, c.readBytes, c.writeOps, c.writeBytes}
}
