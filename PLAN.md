# ccx3 image source plan

## Goal

Add `.simg` and CVMFS image support to `ccx3`, but do it on the right filesystem foundation first.

The immediate priority is no longer “ingest new source kinds as fast as possible.” The immediate priority is:

- stop exploding OCI images into large host `rootfs/` directory trees
- move the runtime to a lazy filesystem model
- use that same model for OCI, `.simg`, and CVMFS

This keeps the VM and API shape stable while fixing the part of the implementation that would otherwise make all new source kinds awkward, wasteful, and especially painful on Windows.

## Guiding constraints

This work should preserve the current public runtime model:

- `pull <name> <source>`
- `vm-start <image>`
- `run <image> ...`

It should also preserve the product shape in `VISION.md`:

- image-backed long-lived VMs
- multi-exec session semantics
- workload-centric API
- unprivileged implementation

The image layer should become more general, but the external VM API should not need to change.

## Why the plan changed

The old plan assumed `.simg` and CVMFS would prepare images into the same on-disk `rootfs` directory layout used by OCI today.

That is no longer the right direction.

The problem is not just `.simg` and CVMFS. The current OCI implementation is already too eager:

- it expands whole merged filesystems to disk
- it uses too much disk space
- it creates a lot of filesystem churn
- it makes Linux metadata and permission behavior harder to preserve cleanly
- it is an especially poor fit for Windows

The better design is captured in [FS_PLAN.md](/Users/joshua/dev/projects/ccx3/FS_PLAN.md:1):

- metadata in memory
- file contents lazy
- content read through offsets or chunk mappings

That is now the prerequisite for the rest of this roadmap.

## Reference interpretation

Based on the reference code:

- `../gosimg` shows that `.simg` is a SIF container with a squashfs payload
- `../tinyrange` shows a useful lazy CVMFS model based on metadata plus chunk-backed reads

Based on the Neurodesk CVMFS documentation:

- the user-facing repo is `neurodesk.ardc.edu.au`
- containers are published under `/cvmfs/neurodesk.ardc.edu.au/containers/...`
- the user-facing launch path ends in `.simg`
- the Neurodesk architecture docs indicate their CVMFS side is populated from unpacked Singularity containers

For `ccx3`, that means:

- real `.simg` files should be supported directly
- CVMFS support should target Neurodesk’s published repository layout
- both should plug into the same lazy filesystem abstraction as OCI

## Desired end state

After this plan:

- OCI images are no longer expanded into a host directory tree
- `.simg` images are read directly from SIF/squashfs structures
- CVMFS images are read from cached metadata plus lazy chunk fetches
- the VM runtime consumes a prepared filesystem abstraction, not just `RootFSDir`
- `pull`, `image-get`, `vm-start`, and `run` still look the same from the outside

## Phase 1: make image sources explicit

Refactor the image store so it understands source kinds even before all preparers exist.

Concrete work:

- add parsed source kinds:
  - `oci`
  - `simg`
  - `cvmfs`
- persist source kind in image metadata
- expose source kind in `ImageState`
- route pull requests through source-aware dispatch
- keep OCI as the only fully implemented preparer initially

Desired outcome:

- the store stops pretending every source is OCI
- the next phases can plug in cleanly

## Phase 2: introduce the shared filesystem abstraction

Implement the common model described in `FS_PLAN.md`.

Concrete work:

- add an in-memory filesystem tree model:
  - path
  - file type
  - mode
  - uid
  - gid
  - size
  - symlink target
- add a content-provider abstraction for regular files:
  - `ReadAt`
  - `Size`
- define a prepared-image contract that exposes:
  - runtime config
  - architecture
  - filesystem tree
  - content providers
- stop treating `RootFSDir` as the universal image contract

Desired outcome:

- the runtime has one filesystem-shaped interface that OCI, `.simg`, and CVMFS can all implement

## Phase 3: refactor OCI onto lazy indexed tar layers

This is now the first major implementation phase.

Concrete work:

- keep downloaded layer blobs
- decompress `.tar.gz` layers once into cached `.tar` files
- walk each tar file once and build an in-memory layer index:
  - path
  - type
  - mode
  - uid
  - gid
  - size
  - link target
  - tar data offset
  - whiteout and opaque directory semantics
- merge layer indexes into one in-memory filesystem tree
- serve regular file contents by seeking directly into cached tar files

Also update:

- host-side command resolution
- virtio-fs integration
- VM startup paths

So they consume the new filesystem abstraction rather than a host directory tree.

Desired outcome:

- OCI no longer writes a merged rootfs to disk
- metadata is in memory
- file contents are read lazily from tar offsets

## Phase 4: add `.simg` support on the shared filesystem model

Once OCI is on the new filesystem path, add `.simg`.

Concrete work:

- parse SIF headers and descriptors
- locate the squashfs payload
- walk squashfs metadata into the common in-memory filesystem tree
- implement a `.simg` content provider that:
  - finds file blocks from squashfs metadata
  - reads only the required blocks
  - decompresses only those blocks

Desired outcome:

- `.simg` support does not require extraction to a host directory
- `.simg` behaves like OCI at the VM boundary

## Phase 5: add Neurodesk-compatible CVMFS support

Build CVMFS on the same filesystem model.

Concrete work:

- support a CVMFS source syntax such as:
  - `http+cvmfs://host/cvmfs/<repo>?path=<root>`
- target Neurodesk’s published layout and mirrors first
- fetch filesystem metadata for the requested root
- build the common in-memory filesystem tree
- implement a CVMFS content provider that:
  - resolves chunk mappings
  - fetches missing chunks lazily
  - serves requested byte ranges from cache or remote

Desired outcome:

- CVMFS remains lazy instead of materializing a whole tree
- Neurodesk containers can be used without requiring host-side CVMFS setup in the first remote-import path

## Phase 6: cache identity and restore semantics

Once multiple lazy source kinds exist, tighten cache rules.

Concrete work:

- define stable shared-cache keys per source kind
- preserve enough metadata to debug source resolution
- separate user-facing source strings from stronger cache identity where available

Desired outcome:

- cache reuse is predictable
- future snapshot work has a safer foundation

## Phase 7: testing and integration coverage

Add tests at both the source-ingestion and VM-runtime layers.

Concrete work:

- source parser tests
- OCI layer index and merge tests
- `.simg` parser and lazy-read tests
- CVMFS metadata and chunk-read tests
- VM integration tests for OCI, `.simg`, and CVMFS-backed images

Desired outcome:

- all image source kinds are validated on the same runtime contract

## Explicit non-goals for this pass

Do not take on these at the same time:

- eager extraction to a host rootfs directory
- lazy runtime guest mounts of native host CVMFS
- a new public VM API shape
- snapshot semantics for the new source kinds
- share or network feature work

Those can build on this work later, but they should not distract from the filesystem foundation.

## Recommended implementation order

1. source kind parsing and metadata
2. shared filesystem abstraction
3. lazy indexed OCI
4. `.simg` backend
5. CVMFS backend
6. cache identity tightening
7. integration coverage
