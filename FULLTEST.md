# PyNeurodesk Fulltest Status

This document reflects the latest completed-suite evidence from the in-progress
GitHub Actions run after `1e7cd2a76b7ccd72a6429fd42b8e9a16a4b6ad88`.

## Current Reference

- Workflow: `PyNeurodesk fulltests`
- Run: https://github.com/tinyrange/cc/actions/runs/24991386248
- Requested job URL: https://github.com/tinyrange/cc/actions/runs/24991386248/job/73177580603
- Requested job name: `spm25`
- Event: `workflow_dispatch`
- Branch: `main`
- Commit: `1e7cd2a76b7ccd72a6429fd42b8e9a16a4b6ad88`
- Run status at collection time: `in_progress`
- Local artifacts: `.tmp-fullsuite-24991386248/`

## Scope

The workflow selected 129 suites. This snapshot excludes the six jobs that were
still not finished when the logs were pulled:

`conn`, `hmri`, `mitkdiffusion`, `qupath`, `spm25`, `syncro`.

That means the linked `spm25` job is not included in the failure breakdown yet.

## Suite Summary

| Result | Previous report | Current completed snapshot |
| --- | ---: | ---: |
| Passed suites | 34 | 41 |
| Failed suites | 92 | 82 |
| Cancelled / unfinished suites | 3 cancelled | 6 in progress |

The current artifact set contains 122 suite artifact directories and 244
download/fulltest log files. The only completed suite without an uploaded
artifact is `topaz`; its raw GitHub job log was pulled separately.

Of the 123 completed suite jobs, 109 reached a final pytest-style suite summary.
Across those summarized logs the totals are:

| Test result | Previous report | Current completed snapshot |
| --- | ---: | ---: |
| Passed tests | 8546 | 8724 |
| Failed tests | 1358 | 871 |
| Skipped tests | 274 | 252 |

## Passing Suites

These suites passed in the current completed snapshot:

`afni`, `amico`, `apptainer`, `bart`, `bidsappbrainsuite`,
`bidsapppymvpa`, `bidsme`, `bidstools`, `brainlifecli`, `brkraw`,
`clearswi`, `convert3d`, `dcm2niix`, `dicomtools`, `elastix`, `fastcsr`,
`fatsegnet`, `fieldtrip`, `fsqc`, `gingerale`, `heudiconv`, `hnncore`,
`julia`, `mne`, `mricrogl`, `niftyreg`, `nighres`, `niimath`, `nipype`,
`palmettobug`, `pcntoolkit`, `quickshear`, `relion`, `slicersalt`,
`spmpython`, `surfice`, `synthstrip`, `tgvqsm`, `vesselvio`, `vmtk`,
`xnat`.

Changes from the previous report:

- Newly passing: `bidsme`, `bidstools`, `brainlifecli`, `brkraw`,
  `fieldtrip`, `heudiconv`, `relion`, `tgvqsm`.
- Regressed from pass to failure: `itksnap`.
- Still passing and still important: `apptainer`.

## Important Changes

The old global SIMG offset-read issue remains fixed. `apptainer` passed again
and completed in about 3 minutes and 51 seconds, with the `Run fulltest` step
taking about 3 minutes and 17 seconds.

The previous report's 403/404 download bucket is no longer reproduced in the
completed artifacts. Former download-only failures such as `brainlifecli` now
run far enough to pass, and several other previously download-blocked suites
now fail later in normal test execution.

The current dominant failure shape is no longer image access. It is mostly
command timeouts, command/path resolution errors, missing expected environment
or output fragments, and a smaller number of image/harness failures.

## Unfinished Suites

The run was still active when this report was generated. These suites are
excluded from counts and categories:

- `conn`: still running.
- `hmri`: still running.
- `mitkdiffusion`: still running.
- `qupath`: still running.
- `spm25`: still running; this is the job linked in the request.
- `syncro`: still running.

## Current Failure Categories

The categories below are not mutually exclusive. A suite can appear in more
than one bucket when it has multiple symptoms.

### Command Timeout / Heavy Tool Startup

Thirty-seven completed suites contain command timeouts or related timeout
symptoms:

`ants`, `batchheudiconv`, `bidsappaa`, `bidsappbaracus`, `bidsappspm`,
`bidscoin`, `cat12`, `code`, `dcm2bids`, `dsistudio`, `ezbids`, `fmriprep`,
`fsl`, `gigaconnectome`, `gimp`, `glmsingle`, `ilastik`, `itksnap`,
`mgltools`, `micapipe`, `mriqc`, `mrsiproc`,
`networkcorrespondancetoolkit`, `neurodock`, `nftsim`, `nibabies`,
`osprey`, `ospreybids`, `physio`, `qsirecon`, `qsmxt`, `root`, `rstudio`,
`sigviewer`, `slicer`, `vesselboost`, `vina`.

Representative examples:

- `ants`: `N4BiasFieldCorrection` and `DenoiseImage` commands time out at
  120 seconds.
- `dsistudio`: atlas track commands time out, including one 300 second timeout.
- `ilastik`: `run_ilastik.sh --version` and `--help` time out at 120 seconds.
- `root`: batch ROOT commands for histogram/tree creation and `hadd` time out.

### VM Boot / Host API Timeout

Only one completed suite currently shows a VM/API timeout signature:

`bidsappspm`.

This is a major reduction from the previous report, which had seven VM boot
timeouts and five host API read timeouts. The unfinished suites may still add
back some of this signal, especially `conn`, `hmri`, `qupath`, and `spm25`.

### Command Resolution / PATH Issues

Five completed suites contain explicit command-resolution failures:

`deepretinotopy`, `freesurfer`, `minc`, `rabies`, `sovabids`.

Examples:

- `deepretinotopy`: cannot resolve `recon-all` and `mri_info`.
- `freesurfer`: cannot resolve `mri_info` and `mri_convert`.
- `rabies`: cannot resolve `fslmaths` and `python`.
- `sovabids`: cannot resolve `sovaconvert` and `sovapply`.

Twelve completed suites also contain `exit code 127`, usually command not
found or missing interpreter/helper paths:

`ashs`, `aslprep`, `bidsappmrtrix3connectome`, `fmriprep`, `fsl`, `laynii`,
`micapipe`, `nibabies`, `oshyx`, `qsiprep`, `rabies`, `tractseg`.

### Missing Files / Recipe Path Assumptions

Five completed suites contain direct missing-file or missing-command messages:

`aidamri`, `ashs`, `mfcsc`, `mritools`, `pydeface`.

Representative examples:

- `ashs`: many recipe paths point at `/../ashs-fastashs_beta/...` and fail
  with `No such file or directory`.
- `aidamri`: Nipype cannot find FSL commands such as `bet`, `flirt`, and
  `mcflirt` from inside its Python interfaces.
- `mfcsc` and `mritools`: expected input/test files are not present.
- `pydeface`: default template and facemask checks fail with missing paths.

### Missing Output / Environment Assertions

Thirty-nine completed suites have missing output fragments or missing expected
environment text:

`ashs`, `batchheudiconv`, `bidsapphcppipelines`, `bidsappspm`, `bidscoin`,
`brainager`, `brainnetviewer`, `brainstorm`, `deepretinotopy`, `dicompare`,
`diffusiontoolkit`, `eeglab`, `fastsurfer`, `freesurfer`, `fsl`,
`gigaconnectome`, `globus`, `halfpipe`, `lashis`, `lcmodel`, `lesymap`,
`linda`, `matlab`, `minc`, `mricron`, `networkcorrespondancetoolkit`,
`nftsim`, `nibabies`, `niistat`, `oshyx`, `ospreybids`, `qmrlab`, `qsmxt`,
`rabies`, `rstudio`, `samsrfx`, `sigviewer`, `spm12`, `vesselapp`.

This remains the broadest category. It mixes real tool failures with recipe
assertions that still expect different install paths, environment variables,
or output banners.

### Image / Import / Harness Failures

Three completed suites failed outside ordinary recipe assertion failures:

- `spinalcordtoolbox`: image import fails with `RuntimeError: index simg: EOF`
  for the downloaded `.simg`; this still looks like a corrupt, truncated, or
  unsupported image.
- `topaz`: the download-resolution Python snippet fails before writing
  `topaz-download.log` with `IndexError: list index out of range`; the upload
  step then also fails because no artifact files exist.
- `sigviewer`: logs include a `binfmt_misc` / helper availability signature,
  indicating an architecture/helper assumption in the runtime or recipe.

The previous `fieldtrip` harness failure is fixed in this snapshot; `fieldtrip`
now passes.

## Failed Completed Suites

The following 82 completed suites failed:

`aidamri`, `ants`, `ashs`, `aslprep`, `batchheudiconv`, `bidsappaa`,
`bidsappbaracus`, `bidsapphcppipelines`, `bidsappmrtrix3connectome`,
`bidsappspm`, `bidscoin`, `brainager`, `brainnetviewer`, `brainstorm`,
`cat12`, `code`, `connectomeworkbench`, `dcm2bids`, `deepretinotopy`,
`dicompare`, `diffusiontoolkit`, `dsistudio`, `eeglab`, `ezbids`,
`fastsurfer`, `fmriprep`, `freesurfer`, `fsl`, `gigaconnectome`, `gimp`,
`glmsingle`, `globus`, `halfpipe`, `hdbet`, `ilastik`, `itksnap`, `lashis`,
`laynii`, `lcmodel`, `lesymap`, `linda`, `matlab`, `mfcsc`, `mgltools`,
`micapipe`, `minc`, `mricron`, `mriqc`, `mritools`, `mrsiproc`,
`mrtrix3tissue`, `networkcorrespondancetoolkit`, `neurodock`, `nftsim`,
`nibabies`, `niistat`, `oshyx`, `osprey`, `ospreybids`, `palm`, `physio`,
`pydeface`, `qmrlab`, `qsiprep`, `qsirecon`, `qsmxt`, `rabies`, `romeo`,
`root`, `rstudio`, `samsrfx`, `sigviewer`, `slicer`, `sovabids`,
`spinalcordtoolbox`, `spm12`, `topaz`, `trackvis`, `tractseg`, `vesselapp`,
`vesselboost`, `vina`.

## Best Next Fixes

1. Investigate the common command-timeout path with one heavy representative
   suite, such as `ants`, `ilastik`, or `root`, and capture guest/server logs
   around command execution.
2. Fix command resolution by checking deploy metadata and shell hook PATH
   construction for `deepretinotopy`, `freesurfer`, `minc`, `rabies`, and
   `sovabids`.
3. Fix the `topaz` selector/download-resolution edge case so it reports a
   clear missing-container error and always uploads logs.
4. Recheck `spinalcordtoolbox_7.2_20251211.simg` by validating the downloaded
   object size/checksum and inspecting it with an external SIF/SIMG tool.
5. Let the six unfinished suites complete or be cancelled, then refresh this
   document once their artifacts are available.
