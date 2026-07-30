package virgl

import (
	"fmt"
	"image"
	"sync"

	"j5.nz/cc/internal/virtio"
)

const maxResourceBytes = 256 << 20

type context struct {
	id        uint32
	resources map[uint32]struct{}
}

type resource struct {
	description virtio.GPUResource3D
	data        []byte
}

type hostBackend interface {
	createContext(id uint32) error
	destroyContext(id uint32) error
	createResource(description virtio.GPUResource3D) error
	unrefResource(id uint32) error
	transferToHost(resource *resource, transfer virtio.GPUTransfer3D) error
	transferFromHost(resource *resource, transfer virtio.GPUTransfer3D) error
	execute(contextID uint32, commands []command, resources map[uint32]*resource) error
	readScanout(resource *resource, rect image.Rectangle) ([]byte, int, error)
	reset() error
	close() error
}

// Renderer is cc's first-party implementation of the VirGL Gallium command
// protocol. Backend GL execution is platform-specific; protocol and object
// validation remain here.
type Renderer struct {
	mu        sync.Mutex
	capsets   []virtio.GPUCapset
	contexts  map[uint32]*context
	resources map[uint32]*resource
	host      hostBackend
}

func NewRenderer(host hostBackend) *Renderer {
	r := &Renderer{
		capsets: []virtio.GPUCapset{{
			ID:      capsetVirGL,
			Version: capsetVersion,
			Data:    buildCapsetV1(),
		}},
		host: host,
	}
	r.Reset()
	return r
}

func (r *Renderer) Capsets() []virtio.GPUCapset {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]virtio.GPUCapset, len(r.capsets))
	for index, capset := range r.capsets {
		result[index] = capset
		result[index].Data = append([]byte(nil), capset.Data...)
	}
	return result
}

func (r *Renderer) CreateContext(id uint32, _ string, _ uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == 0 || r.contexts[id] != nil {
		return fmt.Errorf("invalid VirGL context %d", id)
	}
	if r.host == nil {
		return fmt.Errorf("VirGL host execution backend is unavailable")
	}
	if err := r.host.createContext(id); err != nil {
		return err
	}
	r.contexts[id] = &context{id: id, resources: make(map[uint32]struct{})}
	return nil
}

func (r *Renderer) DestroyContext(id uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.contexts[id] == nil {
		return fmt.Errorf("unknown VirGL context %d", id)
	}
	if err := r.host.destroyContext(id); err != nil {
		return err
	}
	delete(r.contexts, id)
	return nil
}

func (r *Renderer) CreateResource(description virtio.GPUResource3D) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if description.ID == 0 || r.resources[description.ID] != nil {
		return fmt.Errorf("invalid VirGL resource %d", description.ID)
	}
	if r.host == nil {
		return fmt.Errorf("VirGL host execution backend is unavailable")
	}
	if err := r.host.createResource(description); err != nil {
		return err
	}
	r.resources[description.ID] = &resource{description: description}
	return nil
}

func (r *Renderer) UnrefResource(id uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resources[id] == nil {
		return fmt.Errorf("unknown VirGL resource %d", id)
	}
	if err := r.host.unrefResource(id); err != nil {
		return err
	}
	delete(r.resources, id)
	for _, context := range r.contexts {
		delete(context.resources, id)
	}
	return nil
}

func (r *Renderer) AttachResource(contextID, resourceID uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	context := r.contexts[contextID]
	if context == nil || r.resources[resourceID] == nil {
		return fmt.Errorf("cannot attach resource %d to context %d", resourceID, contextID)
	}
	context.resources[resourceID] = struct{}{}
	return nil
}

func (r *Renderer) DetachResource(contextID, resourceID uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	context := r.contexts[contextID]
	if context == nil {
		return fmt.Errorf("unknown VirGL context %d", contextID)
	}
	if _, ok := context.resources[resourceID]; !ok {
		return fmt.Errorf("resource %d is not attached to context %d", resourceID, contextID)
	}
	delete(context.resources, resourceID)
	return nil
}

func (r *Renderer) TransferToHost(transfer virtio.GPUTransfer3D) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	resource := r.resources[transfer.ResourceID]
	if resource == nil {
		return fmt.Errorf("unknown VirGL resource %d", transfer.ResourceID)
	}
	size := transfer.Backing.Size()
	if size > maxResourceBytes {
		return fmt.Errorf("VirGL resource %d backing is %d bytes, limit is %d", transfer.ResourceID, size, maxResourceBytes)
	}
	if uint64(len(resource.data)) != size {
		resource.data = make([]byte, size)
	}
	if err := transfer.Backing.ReadAt(0, resource.data); err != nil {
		return err
	}
	return r.host.transferToHost(resource, transfer)
}

func (r *Renderer) TransferFromHost(transfer virtio.GPUTransfer3D) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	resource := r.resources[transfer.ResourceID]
	if resource == nil {
		return fmt.Errorf("unknown VirGL resource %d", transfer.ResourceID)
	}
	if uint64(len(resource.data)) > transfer.Backing.Size() {
		return fmt.Errorf("VirGL resource %d host data exceeds guest backing", transfer.ResourceID)
	}
	if err := r.host.transferFromHost(resource, transfer); err != nil {
		return err
	}
	return transfer.Backing.WriteAt(0, resource.data)
}

func (r *Renderer) Submit(contextID uint32, stream []byte) error {
	commands, err := decodeCommands(stream)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.contexts[contextID] == nil {
		return fmt.Errorf("unknown VirGL context %d", contextID)
	}
	if r.host == nil {
		return fmt.Errorf("VirGL host execution backend is unavailable")
	}
	return r.host.execute(contextID, commands, r.resources)
}

func (r *Renderer) ReadScanout(resourceID uint32, rect image.Rectangle) ([]byte, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	resource := r.resources[resourceID]
	if resource == nil {
		return nil, 0, fmt.Errorf("unknown VirGL scanout resource %d", resourceID)
	}
	return r.host.readScanout(resource, rect)
}

func (r *Renderer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contexts = make(map[uint32]*context)
	r.resources = make(map[uint32]*resource)
	if r.host != nil {
		_ = r.host.reset()
	}
}

func (r *Renderer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.host == nil {
		return nil
	}
	return r.host.close()
}
