# PyNeurodesk Fulltest Status

This document summarizes the current fulltest state from the latest manual
all-suite run on `main`, the root causes visible in the logs, and the best next
fixes.

## Latest Runs

Current reference commit:

- `12696b37a2cb0629c8d97e6fe0377b6e9d67bc97`
- commit message: `Improve CVMFS fulltest prefetching and streaming`

Green push runs for that commit:

| Workflow | Run | Result |
| --- | --- | --- |
| Unit tests | https://github.com/tinyrange/cc/actions/runs/24932220473 | Success |
| Build wheels | https://github.com/tinyrange/cc/actions/runs/24932233485 | Success |
| PyNeurodesk fulltests, default `niimath` | https://github.com/tinyrange/cc/actions/runs/24932220475 | Success |

Latest all-suite investigation run:

- workflow: `PyNeurodesk fulltests`
- run: https://github.com/tinyrange/cc/actions/runs/24932455460
- event: `workflow_dispatch`
- started: `2026-04-25 13:54:09 UTC`
- finished: `2026-04-26 04:26:26 UTC`
- duration: `14h 32m 17s`
- conclusion: `failure`

## All-Suite Summary

Final suite-job conclusions for run `24932455460`:

| Job conclusion | Count |
| --- | ---: |
| Success | 12 |
| Failure | 101 |
| Cancelled | 16 |

Succeeded suites:

- `bart`
- `convert3d`
- `dcm2niix`
- `fatsegnet`
- `fsqc`
- `gingerale`
- `hnncore`
- `niimath`
- `pcntoolkit`
- `spmpython`
- `synthstrip`
- `vmtk`

Cancelled suites:

- `bidsappmrtrix3connectome`
- `fsl`
- `hmri`
- `matlab`
- `mne`
- `mrsiproc`
- `mrtrix3tissue`
- `neurodock`
- `nibabies`
- `nipype`
- `osprey`
- `ospreybids`
- `qsiprep`
- `samsrfx`
- `spm12`
- `tractseg`

Compared with the previous manual all-suite run, the pass count improved from 6
to 12. The most important positive signal is that the old Python startup
failures are gone, but the packed-prefetch work introduced a new import-time
bug that now blocks many suites before the VM even starts.

## Log Coverage

The run uploaded suite artifacts for 113 jobs. Those are all non-cancelled
suites. The 16 cancelled suites above did not produce final suite logs.

Among the 113 completed suite logs:

- 72 suites reached the final fulltest summary
- those 72 logs contain:
  - `3749` passed tests
  - `2245` failed tests
  - `339` skipped tests
- the remaining 41 suite failures happened before the suite summary, mostly
  during image import or VM startup

## What Improved

### Python startup regression appears fixed

The latest run contains no occurrences of:

- `No module named 'encodings'`
- `Fatal Python error: init_fs_encoding`

That strongly suggests the earlier virtio-fs and CVMFS fixes are holding in CI.
This is a real improvement over the earlier full-suite investigations.

### Direct command discovery is better than before

Explicit `resolve command ... in PATH` failures are down to 14 suites:

- `bidstools`
- `clearswi`
- `elastix`
- `hdbet`
- `laynii`
- `minc`
- `mricrogl`
- `mricron`
- `mritools`
- `niftyreg`
- `palm`
- `rabies`
- `romeo`
- `tgvqsm`

This is still a real bucket, but it is no longer the dominant failure mode.

## Current Root Causes

### 1. Packed CVMFS Prefetch Has A Path-Escaping Regression

This is the biggest new blocker in the current run.

24 suites failed during image import, before the VM launched, in the new packed
`rootfs.contents` prefetch path:

- `afni`
- `bidsappaa`
- `bidsappbaracus`
- `bidsappbrainsuite`
- `bidsapphcppipelines`
- `bidsappspm`
- `brainlifecli`
- `cat12`
- `code`
- `deepretinotopy`
- `dicomtools`
- `fastcsr`
- `glmsingle`
- `halfpipe`
- `ilastik`
- `mgltools`
- `micapipe`
- `oshyx`
- `qsirecon`
- `qsmxt`
- `sovabids`
- `spinalcordtoolbox`
- `vesselvio`
- `vina`

There are three clear signatures:

1. paths containing `#...#` fail with `is a directory`
2. paths containing `GH#42345.pkl` fail with `file does not exist`
3. `dicomtools` fails on `%` with `invalid URL escape "%IS"`

Representative examples from the logs:

- `afni`: `.../bin/#nu_correct#": ".../bin" is a directory`
- `spinalcordtoolbox`: `.../empty_frame_v1_2_4-GH#42345.pkl": file does not exist`
- `dicomtools`: `invalid URL escape "%IS"`

The likely root cause is that the packed-prefetch code is still treating CVMFS
paths as URL-like data instead of opaque filesystem paths, so `#` and `%` are
being interpreted instead of preserved literally.

Important consequence: the earlier `afni -ver` hang is not observable in this
run, because `afni` now dies earlier in packed prefetch.

Recommended next fix:

- make the packed prefetch/index path handling fully opaque to `#` and `%`
- rerun a minimal matrix:
  `afni,dicomtools,spinalcordtoolbox,qsirecon,qsmxt,vina`

### 2. CVMFS Metadata / Published Image Mismatches Still Exist

9 suites failed because the expected image or container directory does not match
what is published in CVMFS:

- `aslprep`
- `dicompare`
- `ezbids`
- `freesurfer`
- `gimp`
- `pydeface`
- `rstudio`
- `slicer`
- `topaz`

Examples:

- `dicompare`: container root does not contain `dicompare_0.1.3_20260202.simg`
- `ezbids`: container root does not contain `ezbids_1.1.0_20260127.simg`
- `gimp`: `read cvmfs container directory: file does not exist`

These are not runtime failures inside the VM. They are image-resolution or
recipe metadata problems.

Recommended next fix:

- inspect the corresponding `local/neurocontainers/recipes/*/fulltest.yaml`
- compare that metadata with the actual CVMFS directory layout
- add fallback resolution only when the published image is unambiguous

### 3. VM Boot / Shell Handshake Timeouts Are Still A Separate Bucket

There are now two distinct startup-time timeout modes.

Explicit VM boot timeout (`504`, `vm boot timed out after 30s`):

- `amico`
- `itksnap`
- `quickshear`

Shell hook load timeout from the client side:

- `bidscoin`
- `dsistudio`
- `qupath`

These happen after import is far enough along to attempt VM startup, but before
the suite can really begin.

Recommended next fix:

- raise the `/vm` startup timeout beyond 30s for large images
- add startup progress logging around guest boot and shell-hook activation
- rerun `amico,itksnap,quickshear,qupath`

### 4. Long Command Timeouts Still Hit Several Suites After Successful Boot

A smaller but still important bucket is commands that do run, but exceed the
fulltest command timeout:

- `ants`: repeated 120s timeouts in `N4BiasFieldCorrection`, `DenoiseImage`,
  and `antsAffineInitializer`
- `root`: many non-interactive `root -b -l -q` and `hadd` commands time out at
  120s
- `vesselboost`: multiple inference steps time out at 120-300s
- `dcm2bids`: several data-creation and `dcm2niix`-driven tests time out at
  120s
- `palmettobug`: many tiny Python import or analysis commands time out at 120s
- `mriqc`, `fmriprep`, `gigaconnectome`, `networkcorrespondancetoolkit` also
  show timeouts

This bucket is mixed:

- some commands are likely just too slow for the current timeout
- some are likely hanging on startup, cache, or environment assumptions
- some Python-heavy containers may still have a deeper import/runtime problem

Recommended next fix:

- add per-command heartbeat/timing output if not already present at the fulltest
  layer
- locally trace one representative suite from each subgroup:
  `ants`, `root`, `palmettobug`, `vesselboost`

### 5. `apptainer` Is Still Mostly A Writable-Home Problem

`apptainer` is close:

- `135` passed
- `5` failed

The failures are still the same writable-home/cache issue:

- `/root/.apptainer/cache`: read-only file system
- `/root/.apptainer/keys`: read-only file system
- remote/keyserver commands also fail because the expected writable config state
  is missing

This remains a direct execution environment issue, not a command discovery
problem.

Recommended next fix:

- provide writable `HOME`, `APPTAINER_CACHEDIR`, and key/config directories in
  the fulltest runtime

### 6. `batchheudiconv` Is No Longer A Python-Boot Failure

`batchheudiconv` now gets much further:

- `91` passed
- `11` failed
- `5` skipped

Current failures are narrower:

- `bh01_prep_dir.sh` times out at 120s
- `numpy._ArrayMemoryError` while allocating about `1.18 GiB`
- six `nib-diff` expectation mismatches
- one output-fragment mismatch
- one script-glob mismatch for `/opt/batch-heudiconv/*.sh`

This is good evidence that the earlier Python-import initialization problem is
not the main blocker anymore.

Recommended next fix:

- keep `batchheudiconv` as a regression suite for Python and CVMFS changes
- treat the remaining failures as script-specific timeout, memory, or recipe
  expectation issues

### 7. `bidsme` Now Looks Like A Data / Recipe Expectation Mismatch

`bidsme` no longer shows Python bootstrap failures. It now fails inside its own
workflow logic:

- `sub-01: No sessions found in: ds000001/sub-01`
- `sub-02` through `sub-15`: `Not found in ds000001`

The tests are expecting a broader dataset structure than the downloaded sample
data provides.

Recommended next fix:

- inspect the `bidsme` recipe expectations against the fetched `ds000001`
  subset
- either adjust the test data setup or narrow the test expectations

### 8. Several MATLAB/MCR Suites Look More Like Layout / Wrapper Mismatches Than VM Bugs

This bucket includes:

- `brainager`
- `brainnetviewer`
- `brainstorm`
- `conn`
- `eeglab`
- `fastsurfer`
- `physio`
- `spm25`

Representative signals:

- `conn` and `spm25` have large numbers of `exit code 127` failures
- `eeglab` mostly fails on missing expected files, directories, and env
  fragments under `/opt/MCR/...` or `/opt/eeglab-2020.0/...`

This looks less like a generic VM failure and more like a mismatch between:

- what the fulltests expect from the packaged container layout or wrapper
  scripts
- what direct execution through `ccvm` actually exposes

Recommended next fix:

- do not treat this as one bug
- split it into:
  - missing wrapper commands / PATH exposure
  - missing container files or env variables
  - stale fulltest expectations

## Near-Pass Suites

These are worth revisiting after the packed-prefetch bug because they are
already close:

- `syncro`: `35` passed, `1` failed
- `qmrlab`: `48` passed, `2` failed
- `linda`: `52` passed, `2` failed
- `lesymap`: `78` passed, `2` failed
- `relion`: `123` passed, `2` failed
- `fmriprep`: `84` passed, `3` failed, `1` skipped
- `heudiconv`: `64` passed, `4` failed
- `apptainer`: `135` passed, `5` failed
- `brkraw`: `77` passed, `6` failed
- `niistat`: `87` passed, `6` failed

## Recommended Work Order

1. Fix the packed-prefetch path escaping bug for literal `#` and `%`.
2. Rerun a small import-focused matrix:
   `afni,dicomtools,spinalcordtoolbox,qsirecon,qsmxt,vina`.
3. Fix writable-home handling for `apptainer`.
4. Raise or instrument VM startup for the boot-timeout bucket:
   `amico,itksnap,quickshear,qupath`.
5. Triage one long-running post-boot suite from each family:
   `ants`, `root`, `palmettobug`, `vesselboost`.
6. Clean up recipe/CVMFS metadata mismatches separately from runtime work.
7. Revisit near-pass suites once the import-time regression is gone.

## Notes For Future Runs

- Keep the default push workflow on `niimath`.
- Use `workflow_dispatch` with `suite=all` only for explicit investigations.
- The latest all-suite run proves that the current matrix can run for more than
  14 hours, so the workflow is now giving full end-state signal instead of only
  early cancellations.
- Right now the packed-prefetch regression is the main thing distorting the
  matrix. Fixing that should immediately convert many pre-summary failures into
  actionable runtime failures.
