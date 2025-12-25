# CrumbleCracker

**Experimental Alpha: Public Preview**

**CrumbleCracker: An experimental embedded virtual machine monitor written in Golang for speed and ease of use.**

## What is CrumbleCracker?

Firstly the name comes from _Apple Crumble Cracker_ meant as a cross platform virtual machine monitor (this is the 15-20th iteration of a project with that name).

CrumbleCracker is a high performance embeddable virtualization platform designed to deeply integrate the operating system running inside the guest machine with the host-side API. That means unlike QEMU we have a direct API for booting a Linux kernel and running commands with different settings inside the machine. Think of CrumbleCracker like a virtualized version of runc or another OCI runtime but designed to be embedded into existing Golang codebases.

## Development Plan

**Stage 1 of development on x86_64 Linux is complete**

**Stage 2 is stability and speed**

- [ ] Fix on macOS arm64
- [ ] Fix on Windows amd64
- [ ] Fix on Linux arm64
- [ ] Fix on Windows arm64
- [ ] Get a more advanced desktop running (like XFCE)
- [ ] Fix Linux Compile Errors (issues with Virtio-fs)
- [ ] Fix Ubuntu boot (issues with `/etc/resolv.conf`)

- [ ] Add benchmarking (both Golang and Tests)
- [ ] Add snapshot support for Linux boot and Benchmark and improve KVM AMD64
- [ ] Benchmark and improve HVF arm64
- [ ] Benchmark and improve KVM arm64
- [ ] Benchmark and improve WHP amd64
- [ ] Benchmark and improve WHP arm64
- [ ] Benchmark and improve Networking
- [ ] Benchmark and improve Filesystem
- [ ] Benchmark and improve Console

**Stage 3 is Developer Experience and Public Beta**

## Cross-Platform Status

Only Linux Guests and bare-metal code are currently supported.

### Platforms

- **Linux x86_64**: Works, **primary platform**.
- **Windows x86_64**: Mostly works (GPU is broken)
- **Linux arm64**: Broken (interupt dispatch issues)
- **Windows arm64**: Broken (interupt dispatch issues)
- **macOS arm64**: Work in Progress.

## Relationship to TinyRange

CrumbleCracker is a VMM (Virtual Machine Monitor) while I see TinyRange as a broader build system. The version in https://github.com/tinyrange/tinyrange has been going though experimental changes over the past 6 months and once CrumbleCracker is more stable I expect to use it as a foundation for a new version.

## Licensing & Contributions

Currently I've decided to license the code under GPL-3.0 in a public preview period. This is intentionally to limit the spread to interested users primarily looking at tools I've developed internally while also enabling derived code to be shared. **I expect the API to change dramatically over time so until that cools down I'll keep the license as GPL-3.0 and won't accept outside code contributions**. Once the API is stable I expect to switch the licensing to something more permissive like Apache-2.0 or MPL.

## Policy on AI Usage

**Note: This is for me. I am not accepting external code contributions at this stage**

The private version of CrumbleCracker has been written mostly with AI models mostly GPT-5.1. This is a extraordinarily capbile model that produces working production ready code but architecture I feel it has been lacking. Over the short span of weeks with the current version of CrumbleCracker it has generated code that is generally difficult to read and maintain. The parts performing well have mostly been hand architecture with AI assistance.

For this version of CrumbleCracker I expect to primarily architect/write the production code myself using AI written code as a foundation.

## Getting Start Notes

To run Alpine in a default VM `./tools/build.go -run -- alpine`.

You can run tests with...

- `./tools/build.go -quest`: Basic tests, should print "Hello, World" at the end.
- `./tools/build.go -bringup`: Advanced Linux Tests, tests FS, Networking, and others.
- `./tools/build.go -bringup-gpu`: GPU Tests, should work on headless systems as well.
- `./tools/build.go -runtest <test>`: Runs a test in the `tests/` dir. These are Dockerfiles meant for advanced tests.
    - Notably...
        - `sway`. When run with `./tools/build.go -runtest sway -- -exec -gpu` should start a window with a Sway desktop.