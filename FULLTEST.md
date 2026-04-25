# PyNeurodesk Fulltest Status

This document summarizes the current PyNeurodesk fulltest status after the
latest `main` changes, plus the current root causes and recommended next steps.

## Latest GitHub Actions Status

Commit `30ee23a52ee18c1ddd3f78a471753ad0fe2bb6db` is green for the regular
push workflows on `main`:

| Workflow | Run | Result |
| --- | --- | --- |
| Unit tests | https://github.com/tinyrange/cc/actions/runs/24927794983 | Success |
| Build wheels | https://github.com/tinyrange/cc/actions/runs/24927839627 | Success |
| PyNeurodesk fulltests, default `niimath` | https://github.com/tinyrange/cc/actions/runs/24927794980 | Success |

The latest all-container investigation run was manual:
https://github.com/tinyrange/cc/actions/runs/24927703861.

It used commit `0901171d76d00c1f450d3b00c0e0d6cccadc7d0d` and was cancelled
after the remaining jobs appeared to be hanging. The cancellation was
intentional.

## All-Container Run Summary

Final job conclusions for run `24927703861`:

| Job conclusion | Count |
| --- | ---: |
| Success | 6 |
| Failure | 110 |
| Cancelled | 13 |

Succeeded suites:

- `bart`
- `convert3d`
- `dcm2niix`
- `dicomtools`
- `itksnap`
- `niimath`

This is an improvement over the earlier baseline run, where only `niimath`
passed. The old dominant `resolve command ... in PATH` failure dropped from 52
suite logs to 23 after the image environment/PATH fixes.

The run also produced much better partial signal before cancellation:

- 98 suite logs reached a full summary.
- 3024 individual tests passed.
- 5334 individual tests failed.
- 548 individual tests were skipped.

Notable partial results:

- `ants`: many ANTs commands now run, including registration and transform
  operations, but the suite still had 120s timeouts and two ANTs command
  failures before cancellation.
- `apptainer`: 135 passed, 5 failed. The remaining failures are home/cache/key
  write behavior.
- `fsl`: 108 passed, 17 failed, 2 skipped.
- `relion`: 122 passed, 3 failed.
- `bidsappmrtrix3connectome`: 117 passed, 4 failed, 3 skipped.

## Current Root Causes

### Runtime Environment Is Still Incomplete

The fulltest path imports a `.simg` and asks `ccvm` to run commands directly
inside it. Neurodesk users normally run commands through CVMFS wrapper scripts
that invoke Singularity and set up container-specific runtime behavior.

The PATH portion of that gap is improved, but the direct-exec path still does
not fully reproduce the Neurodesk/Singularity runtime environment. The remaining
issues are mostly Python, MATLAB/MCR, Java/GUI, writable home/cache, and
tool-specific activation behavior.

### Python Runtime Environment

42 suite logs contain either `No module named 'encodings'` or
`Fatal Python error: init_fs_encoding` in the cancelled all-container run.

Local `batchheudiconv` root cause: this was not primarily missing
`PYTHONHOME`/`PYTHONPATH`. Python startup exposed three lower-level virtio-fs
and CVMFS issues:

- Linux issued `FUSE_STATX`; strict virtio-fs treated the unsupported opcode as
  a server error, leaving the guest waiting instead of receiving file metadata.
- Python importlib first probed `/usr/lib/python3.10/os.py`, partially
  materializing the lazy `/usr/lib/python3.10` directory. A later directory scan
  saw only the partial entry set, so importlib never found the `encodings`
  package.
- Python bytecode reads issued `FUSE_IOCTL`; strict virtio-fs again treated this
  as an internal error instead of replying with an unsupported-ioctl errno.

Fixes made locally:

- Implement `FUSE_STATX` from the existing `GETATTR` data.
- Track lazy image directory completeness separately from partial lookup state.
- Reply `ENOTTY` for unsupported `FUSE_IOCTL`.

Local verification:

- `batchheudiconv` filtered `Python` slice: 7 passed, 0 failed.
- Direct `/usr/bin/python3 -c 'import encodings'` now resolves
  `/usr/lib/python3.10/encodings/__init__.py`.

Remaining Python-adjacent issue: heavier tools such as `heudiconv --version`
perform thousands of tiny CVMFS reads. This was slow because each uncached file
read created a fresh repository view and refetched `.cvmfspublished`. Reusing the
CVMFS repository/cache state dropped local `Heudiconv version check` from a 120s
timeout to 9.66s.

Next step: rerun a small manual matrix containing `batchheudiconv,bidsme` to
confirm the same root causes cover the broader Python failure bucket.

`bidsme` note: a quick local filtered `Python` run after these fixes still
timed out on `python3 -c "import bidsme"` and was interrupted after the first
timeout. That no longer disproves the `encodings` fix; it means `bidsme` needs a
separate focused trace, likely around heavy package import or another runtime
assumption.

### Local `batchheudiconv` Residuals

After the virtio-fs and CVMFS fixes, the full local `batchheudiconv` suite
improved to 91 passed, 11 failed, and 5 skipped.

Remaining failures are now narrower:

- `Create study directory structure` still times out at 120s. The help/version
  commands around it pass, so this is likely a script-specific interactive or
  filesystem-write behavior rather than command discovery.
- `nib-ls BOLD with stats` and `Multi-file NIfTI inspection` fail with NumPy
  `_ArrayMemoryError` trying to allocate 1.18GiB for a large BOLD stats array.
  This may be an expected test-data size problem or a recipe expectation that is
  too memory-hungry for the default fulltest VM.
- Six `nib-diff` tests return exit code 255. These should be reproed as a small
  group now that Python itself works.
- `List all batch-heudiconv scripts` expects `/opt/batch-heudiconv/*.sh`, but
  the container exposes the shell entry points on `PATH`; the glob does not
  match files in that directory. This looks like a recipe/test expectation
  mismatch.

Next step: keep `batchheudiconv` as a regression suite for the Python/CVMFS
fixes, but move the next runtime fix to `bidsme` or `apptainer`.

### Remaining Command Discovery Failures

23 suite logs still contain explicit `resolve command ... in PATH`.

Current suites in this bucket:

- `bidstools`
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
- `mritools`
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

Likely cause: some commands are wrapper-only, dynamically added by activation
scripts, or live outside the env files parsed so far.

Good minimal repro candidates:

- `nipype`
- `oshyx`
- `mritools`

Next step: compare the CVMFS wrapper command, image env scripts, and direct image
filesystem paths. Extend command inference or deploy env loading only where it
matches wrapper behavior.

### Writable Home and Cache

`apptainer` is now mostly working, with 135 passing tests and 5 failures around
cache/key/remote operations. The failing commands try to write under
`/root/.apptainer` or `/root/.apptainer/cache`, but the image root is read-only.

Likely cause: direct execution does not provide the writable root-home/cache
layout expected by Singularity/Neurodesk-style execution.

Next step: run a minimal `apptainer` suite locally and test setting `HOME`,
`APPTAINER_CACHEDIR`, and related cache/config directories to a writable shared
work directory.

### Long Command Hangs and Timeouts

The all-container run was cancelled with these suites still active in
`Run fulltest`:

- `ants`
- `dsistudio`
- `bidsapppymvpa`
- `bidsapphcppipelines`
- `connectomeworkbench`
- `deepretinotopy`
- `brainlifecli`
- `ilastik`
- `linda`
- `lesymap`
- `julia`
- `sigviewer`
- `root`

Many logs show repeated 120s per-command timeouts before cancellation.

Likely causes are mixed:

- GUI, Java, MATLAB/MCR, and ROOT tools waiting for display, cache, license, or
  startup state.
- Workbench and ROOT commands producing little or no output while still running.
- Python-heavy tools blocked by the Python runtime environment problem.

Next step: add command-level heartbeat/timing to the fulltest runner and rerun
one representative quiet suite, such as `connectomeworkbench` or `root`, with
CVMFS tracing enabled.

### CVMFS Release Metadata Mismatches

These suites fail before tests because the expected container directory or
expected `.simg` entry is missing:

- `aslprep`
- `dicompare`
- `ezbids`
- `freesurfer`
- `gimp`
- `pydeface`
- `rstudio`
- `slicer`
- `topaz`

Likely cause: recipe container/version metadata does not match what is published
in CVMFS, or pyneurodesk assumes a single CVMFS directory shape that is not true
for all recipes.

Next step: inspect `local/neurocontainers/recipes/<suite>/fulltest.yaml` and
the corresponding CVMFS directory listing. Add fallback image resolution only
when CVMFS exposes an unambiguous image; otherwise patch or report recipe
metadata upstream.

### External Test Data

`afni` no longer fails because AFNI commands are missing from PATH. In the latest
all-container run it failed before VM execution while downloading required
OpenNeuro data:

```text
httpx.HTTPStatusError: Server error '500 Internal Server Error' for url
https://s3.amazonaws.com/openneuro.org/ds000001/sub-01/anat/sub-01_T1w.nii.gz
```

Likely cause: transient or changed OpenNeuro/S3 data availability, not an AFNI
runtime failure in this run.

Next step: add retry/backoff around required-file downloads, or cache required
test data in the workflow, then rerun `afni`.

### VM Start Gateway Timeouts

`qupath` and `spinalcordtoolbox` hit `504 Gateway Timeout` on `/vm`.

Likely cause: large image import or boot path exceeds the current HTTP timeout,
or the VM start path is too quiet for very large images.

Next step: rerun one of these with CVMFS tracing and a longer boot/import
timeout. Check whether image import completes and whether the VM reaches boot
logs.

### Recipe/Test Expectation Mismatches

Some suites run many commands but fail on missing output fragments, absent files,
or headless/desktop behavior differences.

Likely cause: some fulltest expectations may be stale or written for native
Singularity/desktop behavior rather than direct headless execution.

Next step: defer broad recipe cleanup until the runtime environment gaps are
fixed. Then classify each remaining mismatch as stale recipe, unsupported GUI
behavior, or ccvm/pyneurodesk bug.

## Fixed or Improved Areas

| Area | Status | Evidence | Follow-up |
| --- | --- | --- | --- |
| Default fulltest VM memory | Fixed | Push-triggered `niimath` fulltest passes with the 8GB default. Larger suites boot and run substantial workloads. | Keep `8192MiB` as the fulltest default. |
| amd64 KVM memory mapping above 4GB | Fixed | The earlier `set user memory region: file exists` and high-memory guest-address read failures are no longer present in CI. | Keep regression coverage around high-memory E820 and multi-region guest memory access. |
| Singularity image PATH/environment discovery | Improved | Explicit `resolve command ... in PATH` failures dropped from 52 suites to 23, and 5 extra suites now pass. | Continue extracting/merging container runtime env. |
| Buffered command output obscuring long runs | Improved | `POST /vm/run?stream=1` and pyneurodesk streaming are in place. | Add command-level timing/heartbeat output. |
| CVMFS request observability | Added | Completed suite artifacts include `<suite>-cvmfs.jsonl`; local AFNI repro showed CVMFS requests completed before the command wait. | Summarize slowest CVMFS requests automatically at job end. |
| Python import over virtio-fs | Fixed locally | `batchheudiconv` Python slice passes 7/7 after `FUSE_STATX`, `FUSE_IOCTL`, and lazy-directory cache fixes. | Rerun `bidsme` and the small manual matrix in Actions. |
| CVMFS per-command repository reuse | Fixed locally | `batchheudiconv` `Heudiconv version check` now passes locally in 9.66s instead of timing out at 120s. | Add slow-request summaries so regressions are obvious. |

## Recommended Work Order

1. Rerun `bidsme` locally and in the small manual matrix to validate the Python
   fixes beyond `batchheudiconv`.
2. Fix writable home/cache semantics with `apptainer`.
3. Add command heartbeat/timing and use it on `connectomeworkbench` or `root`.
4. Add robust test-data retry/caching, then rerun `afni`.
5. Triage CVMFS release metadata mismatches separately from runtime failures.
6. Re-run a smaller manual matrix:
   `apptainer,batchheudiconv,bidsme,connectomeworkbench,afni,ants,fsl`.

## Notes for Future Runs

- Default push behavior should continue to run only `niimath`.
- Use manual `workflow_dispatch` with `suite=all` only when runner capacity and
  long runtime are acceptable.
- Completed fulltest jobs upload both the human log and CVMFS JSONL trace:
  `<suite>.log` and `<suite>-cvmfs.jsonl`.
- For long quiet commands, CVMFS trace can distinguish import/fetch delay from
  command execution delay, but command heartbeat is still needed to identify
  process-level hangs.
