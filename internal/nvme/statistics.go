package nvme

// CompletionFailure describes an error completion delivered to guest RAM.
// Status is the NVMe status without its phase bit or wire-format shift.
type CompletionFailure struct{ SQID, CID, Status uint16 }

// IOStatistics is a consistent snapshot of completed read/write commands and
// error completions. Counts persist across controller resets. RecentFailures
// contains at most the last 16 errors, including admin errors; it owns its slice.
type IOStatistics struct {
	ReadCommands, ReadBytes, WriteCommands, WriteBytes uint64
	FailedCompletions                                  uint64
	RecentFailures                                     []CompletionFailure
}

func (c *Controller) IOStatistics() IOStatistics {
	c.mu.Lock()
	defer c.mu.Unlock()
	return IOStatistics{ReadCommands: c.readOps, ReadBytes: c.readBytes,
		WriteCommands: c.writeOps, WriteBytes: c.writeBytes,
		FailedCompletions: c.failedCompletions,
		RecentFailures:    append([]CompletionFailure(nil), c.recentFailures...)}
}
