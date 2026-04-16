# Filesystem Plan

## Goal

Replace the current eager “expand everything into a host `rootfs/` directory” approach with a lazy filesystem model that:

- keeps filesystem metadata in memory
- keeps file contents on disk or remote until read
- serves file contents by offset-based reads
- avoids writing large numbers of host files

This should become the common foundation for:

- OCI layers
- `.simg` images
- CVMFS-backed environments

## Why this is the right direction

The current OCI path writes a fully materialized merged filesystem tree to disk. That has several downsides:

- high disk usage
- lots of filesystem churn
- slow image preparation
- awkward permission handling
- poor fit for Windows, where writing many files with Linux-style metadata is especially fragile

A lazy indexed filesystem improves all of these:

- reduced disk use
- fewer host filesystem operations
- better startup behavior
- easier cross-platform handling of permissions and metadata
- a shared model that works for OCI, `.simg`, and CVMFS

## Core design

The core idea is:

- load all filesystem metadata into memory
- keep file bytes in their source container
- read file contents lazily using offsets or chunk mappings

The runtime-facing filesystem should be built from:

- an in-memory metadata tree
- file nodes that point at content providers

Each regular file should have enough information to answer reads without full extraction.

## Common filesystem model

Define a common in-memory node model with information such as:

- path
- file type
- mode
- uid
- gid
- size
- symlink target
- xattrs later if needed
- source-specific content reference

Directory structure and file metadata should be merged and resolved during image preparation.

Regular file contents should be provided by a source-specific reader interface.

## Content provider model

Each regular file should point to a content provider abstraction roughly like:

- `ReadAt(p []byte, off int64) (int, error)`
- `Size() int64`

Different source kinds can implement this differently:

- OCI: tar file offset reader
- `.simg`: squashfs inode/block reader
- CVMFS: chunk-backed remote reader

This lets the merged filesystem tree stay mostly source-agnostic.

## OCI plan

OCI should be the first source moved to this model.

### Preparation flow

Instead of extracting every layer into a host directory:

- download layer blobs
- decompress each `.tar.gz` blob once into a cached `.tar`
- walk the tar file once to build an in-memory layer index
- record, for each entry:
  - path
  - type
  - mode
  - uid
  - gid
  - size
  - link target
  - tar data offset
  - whiteout and opaque directory semantics

Then merge the layer indexes into one in-memory filesystem tree.

### OCI file reads

For a regular file, the content provider should:

- open the cached tar file
- seek to the indexed data offset
- read only the requested bytes

That means:

- metadata is hot in memory
- file contents stay lazy
- layer blobs become reusable cache artifacts
- we avoid exploding images into thousands of small files

### OCI merge semantics

The merge step should explicitly handle:

- layer precedence
- whiteouts
- opaque directories
- replacement of files by directories and vice versa

The output should be one merged in-memory tree suitable for serving to the guest and for host-side command lookup.

## `.simg` plan

`.simg` should use the same merged-filesystem model, but with a different content provider.

### Preparation flow

- parse the SIF container
- locate the squashfs payload
- walk squashfs metadata
- build an in-memory tree of nodes
- create content providers that read file data directly from squashfs blocks

### `.simg` file reads

Regular files should be read lazily by:

- locating the squashfs inode and block mapping
- reading only the needed compressed blocks
- decompressing only those blocks

This avoids unpacking the `.simg` to a host directory or loading the whole image into memory.

## CVMFS plan

CVMFS should fit the same abstraction as OCI and `.simg`.

### Preparation flow

- fetch or load filesystem metadata
- build an in-memory tree
- attach content providers that reference remote chunks or cached chunk files

### CVMFS file reads

Regular files should be read lazily by:

- resolving chunk lists
- fetching missing chunks into cache
- serving requested byte ranges from those chunks

This means CVMFS can remain lazy and remote-aware without changing the higher-level VM model.

## Runtime integration

The VM runtime should stop assuming that every image has a host `RootFSDir`.

Instead, prepared images should expose something closer to:

- runtime config
- architecture
- filesystem tree
- content providers

The virtio-fs layer should serve this logical filesystem directly.

Host-side command lookup should also stop walking a host directory tree and instead resolve against the in-memory filesystem metadata.

## Cross-platform benefits

This model is especially important for Windows support.

Benefits include:

- avoids writing huge Linux-style directory trees to NTFS
- avoids host permission mismatches
- avoids symlink and special-file headaches during extraction
- reduces the amount of platform-specific filesystem fixup logic

It is also cleaner on macOS and Linux because it reduces:

- inode churn
- metadata writes
- duplicated file content on disk

## Suggested implementation order

1. Introduce a filesystem tree + content-provider abstraction.
2. Refactor OCI to use cached decompressed tar files plus in-memory metadata indexes.
3. Update virtio-fs serving and command resolution to consume that abstraction.
4. Add `.simg` on top of the same model.
5. Add CVMFS on top of the same model.

## Non-goals for the first pass

- full in-memory file contents
- eager extraction to a host directory
- perfect snapshot integration immediately
- xattr support in the first version unless required

The first success criterion is:

- OCI no longer explodes into a host rootfs directory
- metadata is in memory
- file bytes are read lazily from cached tar files
