# CrumbleCracker

**Experimental Alpha: Public Preview**

**CrumbleCracker: An experimental embedded virtual machine monitor written in Golang for speed and ease of use.**

## What is CrumbleCracker?

Firstly the name comes from _Apple Crumble Cracker_ meant as a cross platform virtual machine monitor (this is the 15-20th iteration of a project with that name).

CrumbleCracker is a high performance embeddable virtualization platform designed to deeply integrate the operating system running inside the guest machine with the host-side API. That means unlike QEMU we have a direct API for booting a Linux kernel and running commands with different settings inside the machine. Think of CrumbleCracker like a virtualized version of runc or another OCI runtime but designed to be embedded into existing Golang codebases.

## Development Plan

**Stage 1 of development on x86_64 Linux is complete**

**Stage 2 is stability and speed**

- [x] Fix macOS arm64
- [x] Fix Windows amd64
- [x] Fix Linux arm64
- [ ] Fix Windows arm64
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
- **Linux arm64**: Mostly works (GPU is broken)
- **Windows arm64**: Broken (interupt dispatch issues)
- **macOS arm64**: Works

## Relationship to TinyRange

CrumbleCracker is a VMM (Virtual Machine Monitor) while I see TinyRange as a broader build system. The version in https://github.com/tinyrange/tinyrange has been going though experimental changes over the past 6 months and once CrumbleCracker is more stable I expect to use it as a foundation for a new version.

## Licensing & Contributions

Currently I've decided to license the code under GPL-3.0 in a public preview period. This is intentionally to limit the spread to interested users primarily looking at tools I've developed internally while also enabling derived code to be shared. **I expect the API to change dramatically over time so until that cools down I'll keep the license as GPL-3.0 and won't accept outside code contributions**. Once the API is stable I expect to switch the licensing to something more permissive like Apache-2.0 or MPL.

## Policy on AI Usage

### Update after Stage 1

**Note: I'm posting this for transparency and also some advice for other adopting models at a larger scale (the project is about 80k lines of code)**

The code in this repository has remained about 95% AI coded with a mixture of models (Opus 4.5, GPT-5.2, and a little Composer 1) mostly via Cursor. Honestly this surprised me, weighing up the increase of engineering debt with the development speed improvements is a interesting tradeoff. By that I mean the models are still not great at architecting (4 generations on the same problem have 4 different solutions suggesting they don't settle or meaningfully derive an architecture). But this same approach is great at fixing problems and with the average problem in the project being difficult (on a personal scale) that feels like a easy trade-off while the project is not fully released.

All of this is not without it's flaws. AI is expensive at this scale. I'd estimate I spent about $500-600 USD on Stage 1 in generation credits across the models. Half of that was spent in a single debugging session as Opus 4.5 got stuck on a problem for hours and burned tokens until I stopped it. I also continue to see behavior from models showing a lack of reasoning depth and understanding. The odd thing about that is as the models get more advanced the flaws feel more human. The models don't understand taking a step back on a problem and coming back with a new perspective so a small punt in the right direction is often key to solving hard problems.

I'll leave the accomplishments of the project as a testament to the success of state of the art models with the caveat that most of the core architectural decisions I made despite the models efforts.

A take away and future direction is with the evolution of AI models the quality of tooling becomes essential. The issues I experienced with debugging forced me to invest time into better debug tooling designed to capture dense logging. Also the adoption of Greptile has been surprisingly effective. Besides issues with outdated training data (it likes hating on new methods in Golang and ranking my PRs 1/5 when it sees them) it spots hard to find bugs on a regular basis and the reports are a very helpful read to improve overall code quality. It also encouraged me (indirectly) to protect the main branch so I was forced to make and polish pull requests.

As someone who spends most of their time writing tooling I see a bright future adopting AI models in areas that speed up development but I worry the immaturity of current tooling holds back adoption. That is one of the use cases I see for CrumbleCracker in the future once it's more stable though.

### Original Text

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
