package virtio

import "fmt"

// QueueReady returns true if the queue is ready for processing.
func QueueReady(q *queue) bool {
	return q != nil && q.ready && q.size > 0
}

// DescriptorProcessor processes a single descriptor chain and returns bytes written.
type DescriptorProcessor func(dev device, q *queue, head uint16) (written uint32, err error)

// ProcessQueueNotifications processes all pending queue notifications.
// Returns true if any descriptors were processed (interrupt may be needed).
func ProcessQueueNotifications(dev device, q *queue, processor DescriptorProcessor) (bool, error) {
	if !QueueReady(q) {
		return false, nil
	}

	_, availIdx, err := dev.readAvailState(q)
	if err != nil {
		return false, err
	}

	var processed bool
	for q.lastAvailIdx != availIdx {
		ringIndex := q.lastAvailIdx % q.size
		head, err := dev.readAvailEntry(q, ringIndex)
		if err != nil {
			return processed, err
		}

		written, err := processor(dev, q, head)
		if err != nil {
			return processed, err
		}

		if err := dev.recordUsedElement(q, head, written); err != nil {
			return processed, err
		}
		q.lastAvailIdx++
		processed = true
	}

	return processed, nil
}

// ShouldRaiseInterrupt returns true if an interrupt should be raised.
// Uses the VIRTQ_AVAIL_F_NO_INTERRUPT flag check.
func ShouldRaiseInterrupt(dev device, q *queue, processed bool) bool {
	if !processed {
		return false
	}
	availFlags, _, err := dev.readAvailState(q)
	if err != nil {
		return processed // Fall back to raising on error
	}
	return (availFlags & 1) == 0
}

// ReadDescriptorChain reads all data from a read-only descriptor chain.
// Useful for TX queues where guest provides data to device.
func ReadDescriptorChain(dev device, q *queue, head uint16) ([]byte, error) {
	var data []byte
	index := head
	for i := uint16(0); i < q.size; i++ {
		desc, err := dev.readDescriptor(q, index)
		if err != nil {
			return data, err
		}
		if desc.flags&virtqDescFWrite != 0 {
			return data, fmt.Errorf("unexpected writable descriptor in read chain")
		}
		if desc.length > 0 {
			chunk, err := dev.readGuest(desc.addr, desc.length)
			if err != nil {
				return data, err
			}
			data = append(data, chunk...)
		}
		if desc.flags&virtqDescFNext == 0 {
			break
		}
		index = desc.next
	}
	return data, nil
}

// FillDescriptorChain writes data to a write-only descriptor chain.
// Returns (bytesWritten, bytesConsumed, error).
// Useful for RX queues where device provides data to guest.
func FillDescriptorChain(dev device, q *queue, head uint16, data []byte) (uint32, int, error) {
	index := head
	totalWritten := uint32(0)
	consumed := 0

	for i := uint16(0); i < q.size && consumed < len(data); i++ {
		desc, err := dev.readDescriptor(q, index)
		if err != nil {
			return totalWritten, consumed, err
		}
		if desc.flags&virtqDescFWrite == 0 {
			return totalWritten, consumed, fmt.Errorf("unexpected read-only descriptor in write chain")
		}
		if desc.length > 0 {
			toCopy := int(desc.length)
			remaining := len(data) - consumed
			if toCopy > remaining {
				toCopy = remaining
			}
			if toCopy > 0 {
				if err := dev.writeGuest(desc.addr, data[consumed:consumed+toCopy]); err != nil {
					return totalWritten, consumed, err
				}
				totalWritten += uint32(toCopy)
				consumed += toCopy
			}
			if uint32(toCopy) < desc.length {
				break // Partial fill, descriptor not fully used
			}
		}
		if desc.flags&virtqDescFNext == 0 {
			break
		}
		index = desc.next
	}
	return totalWritten, consumed, nil
}

// ChainFiller fills descriptor chains from pending data.
// Returns (bytesWritten, bytesConsumed, error).
type ChainFiller func(dev device, q *queue, head uint16, data []byte) (uint32, int, error)

// ProcessQueueWithPending fills descriptor chains from pending data until exhausted or no more descriptors.
// Returns (anyProcessed, bytesConsumed, error).
func ProcessQueueWithPending(dev device, q *queue, pending []byte, filler ChainFiller) (bool, int, error) {
	if !QueueReady(q) || len(pending) == 0 {
		return false, 0, nil
	}

	_, availIdx, err := dev.readAvailState(q)
	if err != nil {
		return false, 0, err
	}

	var processed bool
	totalConsumed := 0

	for q.lastAvailIdx != availIdx && totalConsumed < len(pending) {
		ringIndex := q.lastAvailIdx % q.size
		head, err := dev.readAvailEntry(q, ringIndex)
		if err != nil {
			return processed, totalConsumed, err
		}

		written, consumed, err := filler(dev, q, head, pending[totalConsumed:])
		if err != nil {
			return processed, totalConsumed, err
		}

		totalConsumed += consumed

		if err := dev.recordUsedElement(q, head, written); err != nil {
			return processed, totalConsumed, err
		}

		q.lastAvailIdx++
		if written > 0 {
			processed = true
		}
	}

	return processed, totalConsumed, nil
}
