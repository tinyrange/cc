# PyNeurodesk Fulltest Failure Investigation

Investigation of GitHub Actions run
https://github.com/tinyrange/cc/actions/runs/24925156088 from commit
`1a0a706864b07059d90d7090c95c15a96f1349e5`. The run was a manual
`workflow_dispatch` of `suite=all` for the `PyNeurodesk fulltests` workflow.

## Executive Summary

- Matrix planning succeeded and selected all available Neurocontainers fulltest recipes.
- The run was cancelled after 36 minutes, but 129 suite jobs produced logs/artifacts.
- `niimath` was the only suite that passed: 114 passed, 0 failed, 0 skipped.
- Among suite jobs: 119 concluded `failure`, 9 concluded `cancelled`, and 1 concluded `success`.
- The dominant root cause is not 128 independent broken applications. The current pyneurodesk fulltest path imports the `.simg` content and asks `ccvm` to execute commands directly inside that image. Most Neurocontainers are normally launched through CVMFS-generated wrapper scripts that call Singularity and apply runtime environment behavior. That wrapper/Singularity environment is not faithfully reproduced by direct `ccvm` execution.

## Main Root Cause

Neurodesk CVMFS command entries are transparent wrapper scripts, not necessarily binaries inside the image `PATH`. For example, `/containers/afni_26.0.07_20260128/3dinfo` is a host-side script equivalent to:

```bash
singularity --silent exec --cleanenv --env DISPLAY=$DISPLAY $neurodesk_singularity_opts --pwd "$PWD" /cvmfs/neurodesk.ardc.edu.au/containers/afni_26.0.07_20260128/afni_26.0.07_20260128.simg 3dinfo "$@"
```

The pyneurodesk shell wrapper currently ends up calling `ccvm` as a direct image command, e.g. `run(image, ["3dinfo", ...], env=deploy_env, ...)`. For many images this loses the Singularity image environment and/or the Neurodesk transparent wrapper behavior.

This explains the largest groups of failures:

- `resolve command "..." in PATH`: the command exists as a CVMFS wrapper or requires image environment setup, but `ccvm` cannot find it in the direct image `PATH`.
- `No module named encodings` / `init_fs_encoding`: Python inside the direct image starts with incomplete runtime paths, another sign that the Singularity container environment is not being reproduced.
- 120s command timeouts: GUI, Java, R, MATLAB, or wrapper commands hang or wait in a mode that differs from their expected Singularity invocation.

`niimath` passes because its binary is available and simple enough under direct execution. It is not representative of the broader Neurocontainers catalog.

## Category Counts

| Category | Count | Meaning |
| --- | ---: | --- |
| Passed | 1 | Suite passed. |
| Command not in image PATH | 52 | `ccvm` could not resolve one or more recipe commands in the direct image `PATH`. |
| Python runtime missing stdlib/env | 18 | Python starts but cannot import the standard-library `encodings` module. |
| Missing command or install path | 14 | Recipe expects commands/files that are absent from the direct image environment. |
| Command timeout / cancelled partial run | 14 | Tests hit the 120s timeout; cancelled jobs may only have partial logs. |
| Import/load timeout | 8 | Import/load did not finish before the pyneurodesk/httpx timeout. |
| VM boot timeout | 7 | `ccvm` reported `vm boot timed out after 30s`. |
| CVMFS directory missing expected `.simg` | 6 | The recipe-derived CVMFS directory exists but lacks the expected `.simg` entry. |
| CVMFS directory missing | 3 | The recipe-derived CVMFS directory could not be listed. |
| Recipe assertion mismatch | 3 | Command ran, but output differed from the fulltest expectation. |
| Read-only home/cache path | 1 | Command tried to write under read-only `/root`. |
| Unsupported recipe schema | 1 | Recipe uses `script:` tests; pyneurodesk currently requires `command:`. |
| Recipe setup failed | 1 | Suite setup script failed before tests ran. |

## Suite Findings

### Passed

- `niimath`: passed all 114 tests.

### Command Not In Image PATH

These suites mostly failed with `Response body: {"error":"resolve command \"...\" in PATH"}` from `ccvm`. This is the strongest signal for the wrapper/Singularity environment gap.

- `afni`
- `amico`
- `ants`
- `bart`
- `bidsappbaracus`
- `bidsappmrtrix3connectome`
- `bidstools`
- `code`
- `convert3d`
- `dcm2niix`
- `dicomtools`
- `elastix`
- `fastsurfer`
- `fmriprep`
- `fsqc`
- `globus`
- `hdbet`
- `heudiconv`
- `ilastik`
- `itksnap`
- `julia`
- `mgltools`
- `micapipe`
- `minc`
- `mricrogl`
- `mricron`
- `mriqc`
- `mritools`
- `mrtrix3tissue`
- `networkcorrespondancetoolkit`
- `nftsim`
- `niftyreg`
- `nipype`
- `oshyx`
- `palm`
- `palmettobug`
- `pcntoolkit`
- `qsiprep`
- `qsirecon`
- `qupath`
- `rabies`
- `relion`
- `romeo`
- `sovabids`
- `spinalcordtoolbox`
- `spmpython`
- `tgvqsm`
- `tractseg`
- `vesselboost`
- `vesselvio`
- `vina`
- `vmtk`

Representative examples:

- `afni`: `3dinfo`, `3dcalc`, `3dcopy`, and many other AFNI commands were not found in the direct image `PATH`.
- `ants`: `antsRegistration`, `N4BiasFieldCorrection`, `antsApplyTransforms`, `PrintHeader`, and related commands were not found.
- `fmriprep`: failed on a mix of `fmriprep`, FSL, and ANTs commands.

### Python Runtime Missing Standard Library / Environment

These suites commonly show:

```text
Fatal Python error: init_fs_encoding: failed to get the Python codec of the filesystem encoding
ModuleNotFoundError: No module named 'encodings'
```

That means a Python executable was launched, but its runtime library paths were wrong or incomplete under direct `ccvm` execution.

- `batchheudiconv`
- `bidsapphcppipelines`
- `bidsme`
- `brkraw`
- `dcm2bids`
- `fastcsr`
- `glmsingle`
- `matlab`
- `mne`
- `mrsiproc`
- `neurodock`
- `nighres`
- `ospreybids`
- `qsmxt`
- `quickshear`
- `syncro`
- `synthstrip`
- `vesselapp`

Representative example:

- `bidsme`: `/usr/local/bin/python3.9` starts, but `sys.path` only contains `/usr/local/lib/python39.zip`, `/usr/local/lib/python3.9`, and `/usr/local/lib/python3.9/lib-dynload`; `encodings` is unavailable.

### Missing Command Or Install Path

These suites failed with exit code 127, `No such file or directory`, or checks for expected installation paths that are absent in the direct image environment.

- `aidamri`
- `ashs`
- `bidsappaa`
- `bidsappbrainsuite`
- `bidsappspm`
- `conn`
- `dsistudio`
- `hmri`
- `lashis`
- `mfcsc`
- `mitkdiffusion`
- `samsrfx`
- `spm25`
- `trackvis`

This category is likely another face of the main execution-model issue for many suites, but some may also have stale fulltest assumptions.

### Command Timeout / Cancelled Partial Run

These suites hit repeated 120s command timeouts or were cancelled with only partial logs. They should be rerun individually after the wrapper/runtime issue is addressed.

- `bidsapppymvpa`
- `brainnetviewer`
- `cat12`
- `connectomeworkbench`
- `fatsegnet`
- `gingerale`
- `lcmodel`
- `lesymap`
- `linda`
- `niistat`
- `qmrlab`
- `root`
- `sigviewer`
- `spm12`

Cancelled jobs in this group were: `bidsapppymvpa`, `connectomeworkbench`, `fatsegnet`, `gingerale`, `lesymap`, `linda`, `root`, and `sigviewer`.

### Import / Load Timeout

These failed before tests started because image import/load exceeded pyneurodesk/httpx timeout.

- `brainstorm`
- `clearswi`
- `deepretinotopy`
- `fsl`
- `gigaconnectome`
- `halfpipe`
- `nibabies`
- `physio`

The logs end in `httpx.ReadTimeout: timed out` during the load path. These may need longer import timeouts, pre-caching, or smaller test matrix batches.

### VM Boot Timeout

These logs include `vm boot timed out after 30s`.

- `bidscoin`
- `brainager`
- `brainlifecli`
- `hnncore`
- `laynii`
- `osprey`
- `xnat`

`brainlifecli` was also cancelled with partial logs.

### Missing CVMFS Release Entries

These fail before tests run because the recipe-derived CVMFS location cannot be resolved in the shape pyneurodesk expects.

Directory exists but lacks expected `.simg`:

- `aslprep`
- `dicompare`
- `ezbids`
- `freesurfer`
- `pydeface`
- `rstudio`

Directory could not be listed:

- `gimp`
- `slicer`
- `topaz`

These need recipe version correction, CVMFS publication, or pyneurodesk fallback logic against Neurocontainers release metadata.

### Recipe Assertion Mismatch

These commands ran far enough to produce output, but the output did not match fulltest expectations.

- `diffusiontoolkit`
- `eeglab`
- `surfice`

These should be rechecked after the runtime issue is fixed; at least some may be stale expectations rather than pyneurodesk bugs.

### Apptainer Read-Only Home

- `apptainer`: most tests passed, but cache/key tests failed because Apptainer tried to create `/root/.apptainer/cache` or `/root/.apptainer` and the image root is read-only.

Likely fix: set `HOME`, `APPTAINER_CACHEDIR`, and/or `APPTAINER_CONFIGDIR` to a writable shared work directory for this suite.

### Unsupported Recipe Schema

- `fieldtrip`: fails immediately with `KeyError: 'command'`.

The recipe contains tests expressed as `script:` blocks. The pyneurodesk fulltest parser currently assumes every test item has `command`. Supporting `script` tests would require materializing the script into the work directory and invoking the recipe-specific MATLAB runner described by the fulltest file.

### Setup Failed

- `slicersalt`: setup fails with `cp: cannot stat '/opt/SlicerSALT-3.0.0-linux-amd64/share/SlicerSALT-4.11/OrientationMarkers/Human.vtp': No such file or directory`.

This may be a stale recipe path, a missing file in the container, or another runtime-layout mismatch.

## Recommended Next Steps

1. Fix the execution model before chasing per-application failures. The direct `ccvm` path needs to reproduce the Neurodesk/Singularity runtime environment or execute equivalent wrapper semantics.
2. Add parser support for `script:` tests, starting with `fieldtrip`, or explicitly mark unsupported recipe features in the report.
3. Add preflight validation for CVMFS recipe paths so missing directories or missing `.simg` entries fail fast with a clear message before matrix jobs spend runner time.
4. Add writable HOME/cache defaults for suites that expect mutable root-home state, especially `apptainer`.
5. After the runtime compatibility fix, rerun a representative subset:
   - PATH-heavy suite: `afni`
   - Python suite: `bidsme`
   - missing-CVMFS suite: `aslprep`
   - script-suite: `fieldtrip`
   - timeout/cancelled suite: `connectomeworkbench`

