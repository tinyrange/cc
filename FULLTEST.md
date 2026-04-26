# PyNeurodesk Fulltest Status

This document summarizes the current fulltest state after switching the GitHub
Actions fulltest workflow from CVMFS imports to downloaded Neurocontainers
`.simg` images.

## Current Reference

Current local `main` after pulling from GitHub:

- commit: `d1fd2b523ef9f90c3318d6eb5fd9100bb591d7ec`
- commit message: `Merge pull request #2 from tinyrange/feature/windows_amd64`

Green push runs for that commit:

| Workflow | Run | Result |
| --- | --- | --- |
| Unit tests | https://github.com/tinyrange/cc/actions/runs/24955610041 | Success |
| Build wheels | https://github.com/tinyrange/cc/actions/runs/24955713778 | Success |
| PyNeurodesk fulltests, default `niimath` | https://github.com/tinyrange/cc/actions/runs/24955610042 | Success |

Latest all-suite investigation run:

- workflow: `PyNeurodesk fulltests`
- run: https://github.com/tinyrange/cc/actions/runs/24954907956
- event: `workflow_dispatch`
- commit under test: `81ad4c63c43e869665cd15554aec881990f3b6b1`
- started: `2026-04-26 10:55:54 UTC`
- cancelled: `2026-04-26 13:10:44 UTC`
- conclusion: `cancelled`
- reason for cancellation: manual cancellation after enough useful signal was collected

The downloaded artifacts from this run are available locally at:

- `.tmp-fullsuite-24954907956/artifacts`
- `.tmp-fullsuite-24954907956/run.json`

## All-Suite Summary

Job conclusions from run `24954907956`:

| Job conclusion | Count |
| --- | ---: |
| Success | 7 |
| Failure | 111 |
| Cancelled | 12 |

The success count includes the matrix planning job. Suite-level results:

| Suite result | Count |
| --- | ---: |
| Passed | 6 |
| Failed | 111 |
| Cancelled by manual run cancellation | 12 |

Passed suites:

- `bidsapppymvpa`
- `fatsegnet`
- `gingerale`
- `hnncore`
- `niimath`
- `surfice`

Cancelled suites:

- `apptainer`
- `brainlifecli`
- `connectomeworkbench`
- `conn`
- `hmri`
- `lesymap`
- `mne`
- `qmrlab`
- `root`
- `spm12`
- `spm25`
- `synthstrip`

The cancelled suites are not conclusive failures. They were still in progress
when the run was cancelled.

## Log Coverage

The run uploaded artifacts for all 129 selected suites:

- 129 download logs
- 120 suite logs
- 9 suites failed during `.simg` download and therefore did not produce suite
  logs

Among the 120 suite logs:

- 94 reached the final fulltest summary
- 26 failed or were cancelled before a final summary
- total summarized test counts:
  - passed: `3017`
  - failed: `4796`
  - skipped: `618`

## What Changed

The S3 `.simg` workflow removed the previous CVMFS packed-prefetch bottleneck.
The run no longer spends hours walking and packing tiny CVMFS files, and the
download step is straightforward to reason about:

- 120 suites downloaded a `.simg` successfully.
- 9 suites failed during `.simg` download.
- Large images such as `afni` and `qsirecon` moved past acquisition and into
  import or test execution.

This is a cleaner signal than the CVMFS-backed all-suite runs. Most remaining
failures are now in local `.simg` metadata/environment handling, VM startup, or
the fulltest recipes themselves.

## Current Root Causes

### 1. Local `.simg` Imports Do Not Populate Neurodesk Deploy Metadata

This is the dominant new failure mode.

Many suites load the local `.simg` image successfully, activate shell hooks, and
then fail because wrapper commands cannot resolve tools inside the guest PATH:

- representative `afni` error:
  - `RuntimeError: resolve command "3dinfo" in PATH`
- representative `bart` error:
  - `RuntimeError: resolve command "bart" in PATH`
- representative `fsl` error:
  - `RuntimeError: resolve command "fslmaths" in PATH`
- representative `qsirecon` error:
  - `RuntimeError: resolve command "python" in PATH`

Observed in at least 60 suites:

- `afni`
- `amico`
- `ants`
- `bart`
- `batchheudiconv`
- `bidsappbaracus`
- `bidsapphcppipelines`
- `bidscoin`
- `bidstools`
- `brainager`
- `clearswi`
- `code`
- `convert3d`
- `dcm2bids`
- `dcm2niix`
- `deepretinotopy`
- `elastix`
- `fastsurfer`
- `fmriprep`
- `fsl`
- `fsqc`
- `globus`
- `halfpipe`
- `hdbet`
- `heudiconv`
- `ilastik`
- `itksnap`
- `julia`
- `laynii`
- `mgltools`
- `micapipe`
- `minc`
- `mricrogl`
- `mricron`
- `mriqc`
- `mrtrix3tissue`
- `networkcorrespondancetoolkit`
- `neurodock`
- `nftsim`
- `niftyreg`
- `nipype`
- `oshyx`
- `ospreybids`
- `palm`
- `palmettobug`
- `qsiprep`
- `qsirecon`
- `qsmxt`
- `quickshear`
- `qupath`
- `rabies`
- `relion`
- `romeo`
- `sovabids`
- `spmpython`
- `syncro`
- `tgvqsm`
- `tractseg`
- `vesselboost`
- `vesselvio`

Likely root cause:

- CVMFS-backed containers expose `commands.txt`, `env.txt`, and
  `.singularity.d/env/*` through the CVMFS directory layout.
- The local `.simg` import path indexes the SquashFS image but does not produce
  equivalent deploy metadata for pyneurodesk shell wrappers.
- `pyneurodesk.api.load_deploy_metadata` currently reads metadata through CVMFS
  APIs. For local `.simg` references this can fall back to empty deploy env.
- Fulltest still creates shell wrapper commands through `--command`, but many
  wrapper invocations run without the image's Singularity environment, so the
  actual tool is absent from PATH.

Recommended next fix:

- teach local `.simg` imports or pyneurodesk metadata loading to read
  `.singularity.d/env/*` directly from the indexed SIMG filesystem
- expose equivalent deploy env for both CVMFS and local `.simg` sources
- consider storing deploy metadata in cc image metadata at import time so shell
  wrappers do not need to rediscover it through source-specific APIs
- validate with a small matrix:
  `afni,bart,fsl,qsirecon,spmpython`

### 2. Some Neurocontainers S3 `.simg` URLs Are Missing Or Inaccessible

9 suites failed during the new download step:

| Suite | HTTP result | URL |
| --- | ---: | --- |
| `aslprep` | 403 | `https://neurocontainers.s3.us-east-2.amazonaws.com/aslprep_0.7.5_20250206.simg` |
| `dicompare` | 403 | `https://neurocontainers.s3.us-east-2.amazonaws.com/dicompare_0.1.3_20260202.simg` |
| `ezbids` | 403 | `https://neurocontainers.s3.us-east-2.amazonaws.com/ezbids_1.1.0_20260127.simg` |
| `freesurfer` | 403 | `https://neurocontainers.s3.us-east-2.amazonaws.com/freesurfer_8.1.0_20250812.simg` |
| `gimp` | 403 | `https://neurocontainers.s3.us-east-2.amazonaws.com/gimp_2.10.18_*.simg` |
| `pydeface` | 404 | `https://neurocontainers.s3.us-east-2.amazonaws.com/pydeface_2.0.2_20250206.simg` |
| `rstudio` | 403 | `https://neurocontainers.s3.us-east-2.amazonaws.com/rstudio_2023.12.1_20260127.simg` |
| `slicer` | 403 | `https://neurocontainers.s3.us-east-2.amazonaws.com/slicer_5.10.0_20251110.simg` |
| `topaz` | 403 | `https://neurocontainers.s3.us-east-2.amazonaws.com/topaz_0.2.5a_20211006.simg` |

Likely root causes:

- most of these objects are absent or not publicly readable in S3
- `gimp` has a wildcard in the recipe container value, which the workflow turns
  into a literal S3 object name
- some recipes may point at future or unpublished image versions

Recommended next fix:

- decide whether the workflow should support S3 object discovery for wildcard
  container values
- for non-wildcard misses, update Neurocontainers recipes or skip these suites
  until their images are published
- keep download failures distinct from runtime failures in the workflow summary

### 3. HTTP Read Timeouts Still Hide Long-Running Operations

10 suites failed with `httpx.ReadTimeout` before a final fulltest summary:

- `bidsappaa`
- `bidsappspm`
- `dicomtools`
- `mfcsc`
- `nibabies`
- `nighres`
- `osprey`
- `pcntoolkit`
- `spm25`
- `vina`

Additional cancelled or failed suites also show command-level timeout behavior:

- `brainlifecli`
- `connectomeworkbench`
- `hmri`
- `lesymap`
- `mne`
- `qmrlab`
- `root`
- `spm12`
- `synthstrip`
- `xnat`

Likely root cause:

- `run_stream` uses the client's default HTTP timeout when no explicit timeout
  is passed.
- The default read timeout is currently finite, while some commands are quiet
  for several minutes.
- When no streamed event arrives before the read timeout, the client reports
  `httpx.ReadTimeout` even if the guest command may still be doing useful work.

Recommended next fix:

- make streamed VM command calls use a no-read-timeout HTTP stream, like image
  import streaming already does
- keep the fulltest command timeout as the authoritative timeout
- emit periodic heartbeat/progress events from long-running VM commands so CI
  logs distinguish "quiet but alive" from "stuck"
- validate with a small matrix:
  `dicomtools,pcntoolkit,spm25,vina`

### 4. VM Boot Timeout Remains A Smaller Separate Bucket

3 suites reported VM boot timeout:

- `bidsappmrtrix3connectome`
- `hmri`
- `vmtk`

Representative error:

- `vm boot timed out after 30s`

Likely root cause:

- these local `.simg` images are large enough, or their metadata/rootfs setup is
  heavy enough, that first boot can exceed the current 30 second boot timeout
- this is distinct from test command timeouts and from S3 download failures

Recommended next fix:

- inspect boot timing locally for these exact images
- add boot progress logging before increasing the timeout
- if they are consistently booting slowly but successfully, raise the boot
  timeout only for fulltest/CI or make it configurable in the workflow

### 5. One Local `.simg` Indexing Failure

`spinalcordtoolbox` failed during local `.simg` import:

- `RuntimeError: index simg: EOF`

This means the S3 download completed, but the SIMG/SquashFS reader hit EOF while
indexing the image.

Possible causes:

- the downloaded `.simg` is truncated or otherwise corrupt in S3
- the local SquashFS reader has a bug exposed by this image
- the workflow download resumed or completed incorrectly for a very large image

Recommended next fix:

- reproduce locally by downloading
  `spinalcordtoolbox_7.2_20251211.simg`
- compare file size and checksum across repeated downloads
- run the SIMG indexer directly against the file

### 6. Recipe/Test Issues Still Exist Under The Cleaner Runtime

Some failures look like real fulltest recipe or image-content issues rather than
cc runtime failures:

- `fieldtrip`: `KeyError: 'command'`
- `slicersalt`: setup cannot find
  `/opt/SlicerSALT-3.0.0-linux-amd64/share/SlicerSALT-4.11/OrientationMarkers/Human.vtp`
- multiple suites fail expected-output assertions after a command runs but does
  not print the expected text
- several suites fail with normal non-zero exit-code mismatches

These should be triaged after the local `.simg` environment issue is fixed,
because missing deploy env can cause misleading secondary failures.

## Next Steps

Recommended order:

1. Fix local `.simg` deploy metadata/environment loading.
2. Re-run a small matrix: `afni,bart,fsl,qsirecon,spmpython`.
3. Fix `run_stream` HTTP read timeout/heartbeat behavior.
4. Re-run `dicomtools,pcntoolkit,spm25,vina`.
5. Investigate `spinalcordtoolbox` SIMG indexing EOF.
6. Decide how the workflow should handle missing/private S3 objects and wildcard
   recipe container values.
7. Only after those infrastructure issues are resolved, triage individual
   recipe assertion failures.

## Current Interpretation

The S3 `.simg` workflow is the right direction. It avoids the CVMFS prefetch
tail and gives faster, clearer failures. The main regression it exposed is that
local `.simg` sources are not equivalent to CVMFS sources for pyneurodesk deploy
metadata. Fixing that should convert many of the current command-resolution
failures into either passing suites or genuine recipe/test failures.
