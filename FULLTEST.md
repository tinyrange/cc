# PyNeurodesk Fulltest Status

This document reflects the current all-suite GitHub Actions evidence after
`22f649f455aefdbb2c8f58631637624ea3cb2157` (`Speed up SIMG offset reads`).

## Current Reference

- Workflow: `PyNeurodesk fulltests`
- Run: https://github.com/tinyrange/cc/actions/runs/24980229332
- Requested job reviewed: `spm25`
  https://github.com/tinyrange/cc/actions/runs/24980229332/job/73140579417
- Event: `workflow_dispatch`
- Branch: `main`
- Commit: `22f649f455aefdbb2c8f58631637624ea3cb2157`
- Started: `2026-04-27T06:36:13Z`
- Completed: `2026-04-27T08:40:42Z`
- Run conclusion: `cancelled`
- Local artifacts: `.tmp-fullsuite-24980229332/`

## Suite Summary

The workflow selected 129 suites.

| Result | Count |
| --- | ---: |
| Passed | 34 |
| Failed | 92 |
| Cancelled | 3 |

The artifact set is complete for the selected matrix: 129 suite artifacts were
downloaded, containing 248 log files. Of those, 111 suite logs reached a final
pytest-style summary. Across those summarized logs the totals were:

| Test result | Count |
| --- | ---: |
| Passed tests | 8546 |
| Failed tests | 1358 |
| Skipped tests | 274 |

## Passing Suites

These suites passed in the current run:

`afni`, `amico`, `apptainer`, `bart`, `bidsappbrainsuite`,
`bidsapppymvpa`, `clearswi`, `convert3d`, `dcm2niix`, `dicomtools`,
`elastix`, `fastcsr`, `fatsegnet`, `fsqc`, `gingerale`, `hnncore`,
`itksnap`, `julia`, `mne`, `mricrogl`, `niftyreg`, `nighres`, `niimath`,
`nipype`, `palmettobug`, `pcntoolkit`, `quickshear`, `slicersalt`,
`spmpython`, `surfice`, `synthstrip`, `vesselvio`, `vmtk`, `xnat`.

## Important Change

The apptainer offset-read issue is fixed in this run.

Before `22f649f`, the apptainer suite could pass but took about 3 hours and
15 minutes because many reads from the `.simg` image were pathologically slow.
In this run, the apptainer job still passed but completed in about 3 minutes
and 34 seconds, with the `Run fulltest` step taking about 3 minutes and
2 seconds. Local checks before the commit showed `/usr/bin/apptainer` reads
dropping from roughly 47 seconds to 0.64 seconds, and `apptainer --version`
dropping from roughly 66-68 seconds to 0.72 seconds.

That means the remaining failures are no longer explained by the old global
SIMG offset-read problem. The current failures are mostly download availability,
tool-specific timeouts, VM boot/API timeouts, recipe command/path assumptions,
and a small number of harness/import problems.

## Cancelled Suites

The run was cancelled after enough signal had been collected.

- `hmri`: cancelled after about 2 hours and 4 minutes. The log had already hit
  a `504 Gateway Timeout` while trying to run a later shell command.
- `qupath`: cancelled after about 1 hour and 22 minutes. The suite repeatedly
  timed out `QuPath script` commands at the 120 second command timeout.
- `spm25`: this is the job linked in the request. It was cancelled after about
  1 hour and 30 minutes. The log shows repeated `run_spm25.sh` command timeouts
  at 120 seconds, followed later by a VM boot/API timeout.

## Current Failure Categories

The categories below are not mutually exclusive. A suite can appear in more
than one bucket when it has multiple symptoms.

### Download 403/404 Before Fulltest Starts

Ten suites produced only download logs and no suite log. These failures happen
before the fulltest harness gets a usable image.

- `aslprep`: `.simg` download returns HTTP 403.
- `brainlifecli`: `.simg` download returns HTTP 404.
- `dicompare`: `.simg` download returns HTTP 403.
- `ezbids`: `.simg` image is unavailable from the download location.
- `freesurfer`: `.simg` image is unavailable from the download location.
- `gimp`: `.simg` image is unavailable from the download location.
- `pydeface`: `.simg` download returns HTTP 403.
- `rstudio`: `.simg` download returns HTTP 403.
- `slicer`: `.simg` download returns HTTP 403.
- `topaz`: `.simg` download returns HTTP 403.

Root cause: these are image publication or access problems, not guest runtime
failures. The next useful check is to validate the generated Neurodesk
singularity URL for each suite against the expected S3/object path and confirm
whether the object exists and is public.

### Command Timeout / Heavy Tool Startup

Thirty-two suites contain commands timing out, usually at the current 120
second command timeout:

`ants`, `batchheudiconv`, `bidsappspm`, `bidscoin`, `cat12`, `conn`,
`dcm2bids`, `dsistudio`, `fastsurfer`, `fmriprep`, `fsl`, `gigaconnectome`,
`hmri`, `ilastik`, `lesymap`, `mgltools`, `micapipe`, `mriqc`, `mrsiproc`,
`networkcorrespondancetoolkit`, `neurodock`, `nftsim`, `nibabies`, `osprey`,
`ospreybids`, `qsirecon`, `qsmxt`, `qupath`, `root`, `sigviewer`, `spm25`,
`vesselboost`.

Known examples:

- `spm25` repeatedly times out `run_spm25.sh`.
- `qupath` repeatedly times out `QuPath script`.
- `afni` is no longer failing; its slow `afni -ver` path is covered by the
  streaming/prefetch work and passed in this run.

Root cause: this bucket needs per-suite triage. Some commands are genuinely
large first-run startup paths, while others may be blocked on missing data,
interactive startup, bad paths, or the VM becoming unavailable. The next fix
should not simply raise the timeout globally; each representative command
should be reproduced locally with the smallest possible invocation and with
guest/server logs enabled.

### VM Boot / Host API Timeout

Seven suites report `vm boot timed out after 30s`:

`conn`, `fastsurfer`, `hmri`, `ilastik`, `lesymap`, `mrsiproc`, `spm25`.

Five suites also contain host API read timeouts:

`brainstorm`, `conn`, `hmri`, `mrsiproc`, `networkcorrespondancetoolkit`.

Representative symptoms include:

- `httpx.ReadTimeout` while calling `/vm` during `ensure_instance()`.
- `504 Gateway Timeout` with `vm boot timed out after 30s` during shell command
  execution.

Likely root cause: the shell wrapper sometimes has to re-enter VM creation or
startup for the same image after a previous command, and the server does not
return from `/vm` within 30 seconds. This may mean the prior VM exited, the
server is blocked on a large image/startup path, or the guest is slow to become
ready after heavy tool execution. This should be investigated with local
minimal repros before changing the timeout.

### Command Resolution / Deploy Metadata / PATH Issues

Four suites contain explicit `resolve command` failures:

`deepretinotopy`, `minc`, `rabies`, `sovabids`.

Twelve suites contain `exit code 127`, which usually indicates command not
found or a missing interpreter/helper:

`ashs`, `bidsappaa`, `bidsappmrtrix3connectome`, `fmriprep`, `fsl`,
`laynii`, `micapipe`, `nibabies`, `oshyx`, `qsiprep`, `rabies`, `tractseg`.

Root cause: these are likely remaining recipe/deploy metadata mismatches, path
assumptions in the fulltest recipes, or missing wrapper binaries inside the
downloaded `.simg` images. The deploy path/deploy bin metadata should be used
to determine the intended executable path rather than assuming host-visible
absolute paths.

### Missing Files / Recipe Path Assumptions

Four suites contain direct `No such file or directory` failures:

`ashs`, `bidsappaa`, `mfcsc`, `mritools`.

Thirty-eight suites mention missing output fragments, missing expected output,
or similar post-command assertion failures:

`ashs`, `batchheudiconv`, `bidsappaa`, `bidsappbaracus`,
`bidsapphcppipelines`, `bidsappspm`, `bidscoin`, `brainager`,
`brainnetviewer`, `cat12`, `code`, `deepretinotopy`, `diffusiontoolkit`,
`eeglab`, `fsl`, `gigaconnectome`, `glmsingle`, `globus`, `halfpipe`,
`lashis`, `lcmodel`, `linda`, `matlab`, `minc`,
`networkcorrespondancetoolkit`, `nftsim`, `nibabies`, `niistat`, `oshyx`,
`ospreybids`, `qmrlab`, `qsmxt`, `qupath`, `rabies`, `samsrfx`,
`sigviewer`, `spm12`, `vesselapp`.

Root cause: this bucket mixes genuine command failures with recipe assertions
that still expect files, paths, or output text from a different execution
environment. The earlier bash-wrapper and deploy-metadata work reduced this
class, but current logs show more recipe-level cleanup is still needed.

### Exec Permission / Architecture Helper Issue

Two suites contain `exit code 126`:

`bidsappbaracus`, `conn`.

`bidsappbaracus` includes an architecture/helper failure:

```text
/run/ccx3/qemu-x86_64: stat /run/ccx3/qemu-x86_64: no such file or directory
/proc/sys/fs/binfmt_misc/status: open /proc/sys/fs/binfmt_misc/status: no such file or directory
```

Root cause: at least one recipe or command path assumes a QEMU/binfmt setup
inside the guest that is not present in the current CI runtime. This needs a
minimal local repro before deciding whether the fix belongs in cc, the test
recipe, or the image metadata.

### Harness / Image Import Failures

Two suites failed before normal test execution could complete:

- `fieldtrip`: the fulltest loader raises `KeyError: 'command'` while reading
  a test item. Root cause: the recipe contains at least one item without a
  `command` field, and the harness currently reports this as a Python exception
  instead of a clear recipe validation error.
- `spinalcordtoolbox`: image import fails with `RuntimeError: index simg: EOF`
  for `spinalcordtoolbox_7.2_20251211.simg`. Root cause is likely a truncated,
  corrupt, or otherwise unsupported `.simg` image. The next check is to
  redownload that exact file locally, verify size/checksum, and inspect it with
  `apptainer sif list` or equivalent.

## Best Next Fixes

1. Root-cause the VM boot/API timeout bucket with a small local reproduction.
   Good candidates are `fastsurfer`, `lesymap`, or `brainstorm` because they
   fail before producing long recipe-level noise. Capture server logs around
   `/vm`, VM lifetime, and any guest exit.
2. Fix or improve diagnostics for `fieldtrip`. This is a small harness/schema
   issue and should be easy to confirm locally without a long container run.
3. Validate the unavailable `.simg` URLs. This can be separated from runtime
   work by checking generated URLs and object availability directly.
4. Investigate `spinalcordtoolbox_7.2_20251211.simg` as a possible corrupt or
   unsupported image. If the image is bad, classify it with the download/image
   publication failures.
5. Continue deploy metadata and recipe path cleanup for the command-resolution
   and `exit code 127` buckets. Use minimal command reproductions and the
   recipe `deploy_path`/`deploy_bins` metadata to identify exact fixes.
