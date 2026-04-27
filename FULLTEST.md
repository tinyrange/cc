# PyNeurodesk Fulltest Status

This document summarizes the latest all-suite GitHub Actions fulltest run after
switching fulltests to downloaded Neurocontainers `.simg` files and adding local
SIMG deploy metadata extraction.

## Current Reference

Latest all-suite run analyzed:

- workflow: `PyNeurodesk fulltests`
- run: https://github.com/tinyrange/cc/actions/runs/24959155154
- event: `workflow_dispatch`
- branch: `main`
- commit: `2eeead69b74be495361aa407d686afdddf8fe2b6`
- commit message: `Load deploy metadata from local simg images`
- started: `2026-04-26 14:35:56 UTC`
- completed: `2026-04-26 22:18:43 UTC`
- conclusion: `failure`

Green runs for the same commit:

| Workflow | Run | Result |
| --- | --- | --- |
| Unit tests | https://github.com/tinyrange/cc/actions/runs/24958940485 | Success |
| Build wheels | https://github.com/tinyrange/cc/actions/runs/24958955813 | Success |
| PyNeurodesk fulltests, default `niimath` | https://github.com/tinyrange/cc/actions/runs/24958940488 | Success |

Local downloaded artifacts for the all-suite run:

- `.tmp-fullsuite-24959155154/artifacts`
- `.tmp-fullsuite-24959155154/run.json`

## Suite Summary

The workflow selected 129 suites.

| Suite result | Count |
| --- | ---: |
| Passed | 16 |
| Failed | 112 |
| Cancelled | 1 |

Passed suites:

- `afni`
- `amico`
- `apptainer`
- `bidsappbrainsuite`
- `bidsapppymvpa`
- `convert3d`
- `dcm2niix`
- `dicomtools`
- `fatsegnet`
- `fsqc`
- `gingerale`
- `hnncore`
- `nighres`
- `spmpython`
- `surfice`
- `vmtk`

Cancelled suite:

- `synthstrip`

The `synthstrip` job ran for about 6 hours and ended as `cancelled`. It did not
upload a suite artifact, so there is no useful suite-level failure signal yet.

## Log Coverage

Artifacts downloaded from run `24959155154` contain:

- 128 download logs
- 119 suite logs
- 9 suites failed before producing suite logs because their `.simg` download
  returned HTTP 403
- 109 suite logs reached the final `Suite:` summary block

Summed across the 109 logs with final summaries:

| Test result | Count |
| --- | ---: |
| Passed | 5545 |
| Failed | 3759 |
| Skipped | 549 |

## What Improved

The local SIMG deploy metadata fix had a clear effect.

Suites that previously failed at command resolution now execute real tools and
pass, including:

- `afni`
- `amico`
- `convert3d`
- `dcm2niix`
- `fsqc`
- `spmpython`

Representative improvement:

- `afni` now passes all `113` tests.
- The previous `RuntimeError: resolve command "3dinfo" in PATH` is gone.
- Large command output such as `afni -ver` is no longer the blocker for this
  suite.

The remaining failures are now more meaningful: they are mostly real tool
timeouts, incomplete deploy metadata for some images, recipe path assumptions,
or missing upstream `.simg` objects.

## Failure Categories

The categories below are primary buckets based on the dominant symptom in each
suite log. Some suites show secondary issues too, but each suite is counted once
here so the totals sum to the 112 failed suites.

| Category | Count |
| --- | ---: |
| Per-test command timeout or heavy startup | 35 |
| Remaining deploy `PATH` / `DEPLOY_BINS` issue | 21 |
| Recipe absolute path or missing file assumption | 20 |
| Download 403 / missing S3 image | 9 |
| Recipe assertion or tool behavior mismatch | 8 |
| VM boot timeout | 6 |
| External runtime, license, GUI, or service dependency | 6 |
| Host API read timeout | 3 |
| Runtime/application error | 3 |
| SIMG import/index EOF | 1 |

## Current Root Causes

### 1. Per-Test Command Timeout Or Heavy Startup

These suites import and load, but many individual commands hit the fulltest
per-command timeout, usually `120.0 seconds`.

Affected suites:

- `ants`
- `bart`
- `batchheudiconv`
- `bidsappspm`
- `bidscoin`
- `brainager`
- `brainlifecli`
- `brainstorm`
- `connectomeworkbench`
- `dcm2bids`
- `fastcsr`
- `fastsurfer`
- `gigaconnectome`
- `heudiconv`
- `hmri`
- `ilastik`
- `itksnap`
- `julia`
- `lesymap`
- `linda`
- `mriqc`
- `mrsiproc`
- `networkcorrespondancetoolkit`
- `neurodock`
- `palmettobug`
- `pcntoolkit`
- `qmrlab`
- `qsirecon`
- `root`
- `samsrfx`
- `sigviewer`
- `spm12`
- `syncro`
- `vesselboost`
- `xnat`

Representative errors:

- `bart version` timed out after `120.0 seconds`.
- `bl version` and most `brainlifecli` help commands timed out after
  `120.0 seconds`.
- many `root -b -l -q ...` invocations timed out after `120.0 seconds`.
- several SPM/MATLAB Runtime commands timed out or ran long enough that
  downstream tests failed.

Interpretation:

- The wrapper and image import path are mostly working for these suites.
- The next thing to root cause is whether commands are genuinely too slow,
  blocked on first-run initialization, waiting for input, writing excessive
  output, or stuck behind VM/runtime startup overhead.
- Do not simply raise the timeout globally without inspecting representative
  commands locally.

Recommended next tests:

- `bart`: run only `bart version` and `bart bitmask 0 1 2`.
- `brainlifecli`: run only `bl version` and `bl --help`.
- `root`: run only `root --version` and a one-line batch command.
- `spm12`: run one `run_spm12.sh` version/help command.

### 2. Remaining Deploy `PATH` / `DEPLOY_BINS` Issues

These suites still fail with `RuntimeError: resolve command "..." in PATH`.

Affected suites:

- `clearswi`
- `code`
- `deepretinotopy`
- `elastix`
- `hdbet`
- `laynii`
- `mgltools`
- `minc`
- `mricrogl`
- `mricron`
- `mrtrix3tissue`
- `niftyreg`
- `nipype`
- `oshyx`
- `palm`
- `rabies`
- `romeo`
- `sovabids`
- `tgvqsm`
- `vesselvio`
- `vina`

Representative errors:

- `clearswi`: `RuntimeError: resolve command "julia" in PATH`
- `mgltools`: command resolution failures for tools expected by the recipe
- `vina`: command resolution failures for `vina`

Interpretation:

- The general SIMG metadata fix works, because many former PATH failures now
  pass.
- These remaining images likely have incomplete top-level `deploy.path`,
  incomplete `deploy.bins`, environment setup in non-standard files, or recipes
  that call secondary tools not present in `DEPLOY_BINS`.
- Some recipes have only `deploy.bins` and no useful `deploy.path`, which is
  not enough for command resolution inside cc.

Recommended next fix:

- inspect each affected recipe `build.yaml`
- compare top-level `deploy.path` / `deploy.bins` against the commands used in
  `fulltest.yaml`
- update recipes where the image metadata is incomplete
- if metadata is present in another image file, extend SIMG metadata extraction
  to read it rather than hard-coding per-suite behavior

### 3. Recipe Absolute Path Or Missing File Assumptions

These suites run commands but fail because expected files, directories, or
absolute paths are missing.

Affected suites:

- `aidamri`
- `ashs`
- `bidsappaa`
- `bidsappbaracus`
- `bidsapphcppipelines`
- `bidsappmrtrix3connectome`
- `bidsme`
- `fmriprep`
- `lashis`
- `mfcsc`
- `micapipe`
- `mitkdiffusion`
- `mne`
- `mritools`
- `nftsim`
- `nibabies`
- `osprey`
- `qsiprep`
- `slicersalt`
- `tractseg`

Representative errors:

- `ashs`: `/../ashs-fastashs_beta/ext/Linux/bin/greedy: No such file or directory`
- `ashs`: `/../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory`
- `aidamri`: missing Allen Brain Atlas files and missing FSL-derived outputs
- `bidsapphcppipelines`: missing expected `FREESURFER_HOME`,
  `HCPPIPEDIR`, and `CARET7DIR` fragments
- `qsiprep`: MRtrix command tests return `127`

Interpretation:

- These are probably recipe/test issues rather than core cc failures.
- Many are absolute paths that made assumptions about the Neurodesk shell
  environment or container filesystem layout.
- The fix should be in the relevant Neurocontainers `fulltest.yaml` or recipe
  metadata after verifying the exact installed paths in the image.

### 4. Download 403 / Missing S3 Image

These suites failed during the download step and did not produce suite logs.

Affected suites:

- `aslprep`
- `dicompare`
- `ezbids`
- `freesurfer`
- `gimp`
- `pydeface`
- `rstudio`
- `slicer`
- `topaz`

Representative error:

- `curl: (22) The requested URL returned error: 403`

Interpretation:

- The workflow constructed an S3 `.simg` URL, but the object is missing,
  private, or blocked.
- These failures are outside cc runtime behavior until the artifacts are
  available.

Recommended next fix:

- verify generated S3 URLs for these containers
- compare Neurodesk release metadata against actual object names
- either publish/fix the `.simg` objects or skip these suites with an explicit
  reason until images are available

### 5. VM Boot Timeout

These suites include a `504 Gateway Timeout` from `/vm` with
`vm boot timed out after 30s`.

Affected suites:

- `fsl`
- `niimath`
- `ospreybids`
- `physio`
- `qupath`
- `spm25`

Representative error:

- `httpx.HTTPStatusError: Server error '504 Gateway Timeout' for url ... /vm`
- response body: `{"error":"vm boot timed out after 30s"}`

Interpretation:

- This is now separate from the earlier KVM/platform issue. The VM can boot for
  many suites, but these jobs hit the daemon's 30 second boot readiness limit at
  least once.
- `qupath` also has many command-level 120 second timeouts after it gets far
  enough to run tests, so some suites may have mixed boot and runtime slowness.

Recommended next fix:

- locally reproduce with one of the smallest affected suites, likely `niimath`
  or `fsl`
- capture serial/dmesg boot timing
- determine whether the 30 second server-side VM boot timeout is too low for
  large local SIMG images or whether the guest is stuck doing avoidable work

### 6. Host API Read Timeout

These suites failed through the Python HTTP client waiting on the cc daemon.

Affected suites:

- `bidstools`
- `qsmxt`
- `quickshear`

Representative error:

- `httpx.ReadTimeout: timed out`

Interpretation:

- This is different from individual test subprocess timeouts.
- The client waited too long for a daemon response. That may mean the daemon is
  still running a command without streaming enough progress, or the server-side
  timeout path is not reporting a structured command failure promptly.

Recommended next fix:

- reproduce one small command from `quickshear` or `bidstools`
- check daemon-side `run_stream` behavior when the guest command runs long or
  produces little output
- make sure long-running commands continuously stream progress or fail with a
  clear timeout event

### 7. External Runtime, License, GUI, Or Service Dependency

These suites fail in ways that look tied to external runtime prerequisites,
GUI/headless behavior, service setup, or license-style assumptions.

Affected suites:

- `brainnetviewer`
- `dsistudio`
- `eeglab`
- `globus`
- `lcmodel`
- `matlab`

Representative symptoms:

- `brainnetviewer`: `DEPLOY_BINS` mismatch and binary size expectation mismatch
- MATLAB/MCR-related suites: commands time out or fail during runtime startup
- GUI-oriented tools: help/version checks do not produce expected output in the
  headless CI environment

Interpretation:

- Some of these may be fixable with recipe environment changes such as
  `QT_QPA_PLATFORM=offscreen`, MCR cache/home setup, or corrected deploy bins.
- Others may need tests to avoid license/service-dependent behavior.

### 8. Runtime/Application Error

These suites are not obviously metadata, download, or boot failures. The tool
starts far enough to return application-level errors.

Affected suites:

- `brkraw`
- `fieldtrip`
- `vesselapp`

Representative symptoms:

- `brkraw`: `nib-conform` commands return exit code `1`
- `vesselapp`: Python package/version checks fail and expected executable paths
  do not match

Interpretation:

- Treat these as recipe/tool-specific root causes.
- Confirm installed versions and entrypoint paths inside the image before
  changing cc.

### 9. Recipe Assertion Or Tool Behavior Mismatch

These suites have failures that look like expected-output mismatches, command
exit codes from the underlying tool, or overly broad fulltest expectations.

Affected suites:

- `cat12`
- `conn`
- `diffusiontoolkit`
- `glmsingle`
- `halfpipe`
- `niistat`
- `relion`
- `trackvis`

Representative symptoms:

- `conn`: many commands exit `126`
- `cat12`: batch preparation and segmentation tests fail
- `relion`: `CTFFIND version info` exits `255`

Interpretation:

- These need suite-by-suite inspection.
- They are lower priority than the common infrastructure buckets because the
  failures are less obviously shared.

### 10. SIMG Import/Index EOF

Affected suite:

- `spinalcordtoolbox`

Representative error:

- `RuntimeError: index simg: EOF`

Interpretation:

- The download step completed, but SquashFS/SIMG indexing failed.
- This may be a truncated/corrupt `.simg`, an unsupported SquashFS layout, or a
  bug in the SIMG reader.

Recommended next fix:

- download the exact `spinalcordtoolbox` image locally
- verify file size/checksum if upstream metadata is available
- run the SIMG indexer locally with focused logging

## Recommended Next Work

Highest leverage order:

1. Fix the remaining deploy metadata gaps for the 21 suites still failing with
   `resolve command ... in PATH`. This is the clearest shared infrastructure
   issue left after the SIMG metadata change.
2. Root cause one small per-command timeout suite locally, preferably `bart`,
   before changing timeouts. If `bart version` hangs locally, inspect guest
   process state and stdout/stderr behavior.
3. Investigate the `vm boot timed out after 30s` failures with `niimath` or
   `fsl`, capturing serial output to distinguish slow-but-healthy boot from a
   stuck guest.
4. Fix or skip the 9 S3 403 image downloads once the expected object names are
   verified.
5. Continue moving recipe absolute path fixes into Neurocontainers once each
   path mismatch is confirmed inside the relevant image.
