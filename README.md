# CrumbleCracker

**Experimental Alpha: Public Preview**

**CrumbleCracker: An experimental embedded virtual machine monitor written in Golang for speed and ease of use.**

## What is CrumbleCracker?

Firstly the name comes from _Apple Crumble Cracker_ meant as a cross platform virtual machine monitor (this is the 15-20th iteration of a project with that name).

CrumbleCracker is a high performance embeddable virtualization platform designed to deeply integrate the operating system running inside the guest machine with the host-side API. That means unlike QEMU we have a direct API for booting a Linux kernel and running commands with different settings inside the machine. Think of CrumbleCracker like a virtualized version of runc or another OCI runtime but designed to be embedded into existing Golang codebases.

## Development Plan

- [x] Setup build and testing tools including continuous benchmarking and cross-platform testing.
- [ ] Add a assembler and compiler for running small programs.
- [ ] Write a basic x86_64 KVM abstraction and a start to a **Bringup Quest** with VCPU, MMIO, and IO support.
- [ ] Add a serial device and boot a Linux kernel from Alpine Linux into a minimal Init program written using the assembler.
- [ ] Add automatic downloader for Alpine Linux kernels loading necessary kernel modules.
- [ ] Add `virtio-mmio` support starting with a `virtio-console` driver.
- [ ] Write a more advanced init program supporting multiple entries into the virtual guest.
- [ ] Add a downloader for OCI images to pull a root filesystem for the virtual machine.
- [ ] Add support for `virtio-block` and my Ext4 driver derived from TinyRange2 to run simple binaries from the OCI image.
- [ ] Add snapshotting support with support for capturing multiple snapshots tied to a MMIO control device.
- [ ] Add a custom optimized TCP/IP stack tied to a `virtio-net` driver to add network support without privileges.
- [ ] Add filesystem sharing using `virtio-fs`.

## Relationship to TinyRange

CrumbleCracker is a VMM (Virtual Machine Monitor) while I see TinyRange as a broader build system. The version in https://github.com/tinyrange/tinyrange has been going though experimental changes over the past 6 months and once CrumbleCracker is more stable I expect to use it as a foundation for a new version.

## Licensing & Contributions

Currently I've decided to license the code under GPL-3.0 in a public preview period. This is intentionally to limit the spread to interested users primarily looking at tools I've developed internally while also enabling derived code to be shared. **I expect the API to change dramatically over time so until that cools down I'll keep the license as GPL-3.0 and won't accept outside code contributions**. Once the API is stable I expect to switch the licensing to something more permissive like Apache-2.0 or MPL.

## Policy on AI Usage

**Note: This is for me. I am not accepting external code contributions at this stage**

The private version of CrumbleCracker has been written mostly with AI models mostly GPT-5.1. This is a extraordinarily capbile model that produces working production ready code but architecture I feel it has been lacking. Over the short span of weeks with the current version of CrumbleCracker it has generated code that is generally difficult to read and maintain. The parts performing well have mostly been hand architecture with AI assistance.

For this version of CrumbleCracker I expect to primarily architect/write the production code myself using AI written code as a foundation.
