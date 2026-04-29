# Tinyrange-Only Fulltest Root Cause Buckets

Sources:

- Tinyrange run: https://github.com/tinyrange/cc/actions/runs/24998530139
- Neurocontainers ground-truth run: https://github.com/neurodesk/neurocontainers/actions/runs/24996818185
- Downloaded artifacts: `.tmp-actions-logs/`

Scope: only the 835 tinyrange failures whose matching neurocontainers test passed. Since the recipe and image-level tests pass under Apptainer, these should be treated as tinyrange/runtime compatibility gaps until proven otherwise.

## Summary

| Bucket | Tests | Suites | First fix target |
| --- | ---: | ---: | --- |
| Command discovery / wrapper PATH gaps | 162 | 12 | `pyneurodesk/src/pyneurodesk/api.py` deploy metadata loading, `pyneurodesk/src/pyneurodesk/shell.py` wrapper generation, and `internal/imagefs/imagefs.go` command resolution. |
| Deploy/env variables not propagated | 31 | 11 | Projection of Neurodesk `DEPLOY_ENV_*`, image `.singularity.d/env`, and build metadata into runtime env. |
| FSL environment not initialized | 46 | 1 | FSL-specific activation from Singularity env files and `DEPLOY_ENV_*` handling; likely same root as broader deploy env propagation. |
| Install-root path collapsed to /../... | 47 | 1 | Singularity/env path parsing and BASEPATH/deploy metadata normalization in `pyneurodesk/src/pyneurodesk/api.py`. |
| Python package/import environment missing | 89 | 2 | Container environment activation, Python/conda PATH and PYTHONPATH propagation, and deploy env parsing. |
| Workdir/share or generated fixture visibility | 123 | 6 | Implicit share/workdir mounting from `pyneurodesk/src/pyneurodesk/shell.py` into the VM backend and hostfs path mapping. |
| Timeouts / slow execution under tinyrange | 188 | 32 | VM/runtime performance, process startup overhead, I/O throughput, CPU allocation, and `pyneurodesk/src/pyneurodesk/fulltest.py` timeout/resource defaults. |
| Process killed / memory pressure | 5 | 3 | VM memory sizing, cgroup/oom behavior, and fulltest `--memory-mb` defaults. |
| Executable format / binfmt handling | 1 | 1 | Architecture selection, binfmt/qemu handling, or an invalid command entry being exposed as executable. |
| Command behavior/output mismatch after launch | 142 | 36 | Usually downstream of missing env/workdir setup; after the larger buckets are fixed, re-run these to separate real runtime behavior differences from cascades. |
| Other / needs targeted repro | 1 | 1 | Re-run after larger buckets are fixed; likely becomes obvious once cascades are removed. |

## Fix Order

1. Fix command discovery and deploy/env projection first. These two areas explain the large FreeSurfer/RABIES/DeepRetinotopy, SPM/MCR, FSL, ASHS, and Python import clusters, and likely collapse many apparent output mismatches.
2. Fix workdir/share visibility next. TrackVis and PALM alone account for 112 failures that look like generated fixtures are not visible from inside the command.
3. Then address runtime speed and memory. The timeout and kill buckets are real tinyrange-vs-Apptainer differences, but they are easier to tune once PATH/env/share behavior is stable.
4. Re-run the remaining command-output bucket last; many entries are likely cascades from the earlier root causes.

## Command discovery / wrapper PATH gaps

Tests: 162 across 12 suites.

Evidence: The tool exists for Apptainer but tinyrange cannot resolve it from PATH, so wrapper metadata or PATH reconstruction is incomplete.

Likely code surface: `pyneurodesk/src/pyneurodesk/api.py` deploy metadata loading, `pyneurodesk/src/pyneurodesk/shell.py` wrapper generation, and `internal/imagefs/imagefs.go` command resolution.

### Affected Suites

| Suite | Tests |
| --- | ---: |
| `freesurfer` | 51 |
| `deepretinotopy` | 48 |
| `rabies` | 39 |
| `qsiprep` | 7 |
| `bidsappmrtrix3connectome` | 4 |
| `tractseg` | 4 |
| `fsl` | 3 |
| `laynii` | 2 |
| `aslprep` | 1 |
| `fmriprep` | 1 |
| `micapipe` | 1 |
| `nibabies` | 1 |

### Representative Failures

| Suite | Test | Tinyrange symptom |
| --- | --- | --- |
| `aslprep` | fslcpgeom - copy geometry | exit code 127, want 0 / bash: line 3: fslcpgeom: command not found |
| `bidsappmrtrix3connectome` | label2colour convert | exit code 127, want 0 / mrcalc: [00;31m[WARNING] existing output files will be overwritten[0m |
| `deepretinotopy` | FreeSurfer help | missing output fragment 'FreeSurfer' |
| `fmriprep` | FSL fslsplit temporal | exit code 127, want 0 / bash: line 2: fslsplit: command not found |
| `freesurfer` | AntsN4BiasFieldCorrectionFs basic | exit code 1, want 0 / Traceback (most recent call last): |
| `fsl` | fsl-cluster - find clusters | exit code 127, want 0 / bash: line 2: fsl-cluster: command not found |
| `laynii` | LN2_CONNECTED_CLUSTERS labeling | exit code 127, want 0 / bash: line 3: LN2_CONNECTED_CLUSTERS: command not found |
| `micapipe` | FreeSurfer mri_vol2vol identity | exit code 127, want 0 / bash: line 2: mri_vol2vol: command not found |

### Tests In Bucket

| Suite | Test | Symptom |
| --- | --- | --- |
| `aslprep` | fslcpgeom - copy geometry | exit code 127, want 0 / bash: line 3: fslcpgeom: command not found |
| `bidsappmrtrix3connectome` | label2colour convert | exit code 127, want 0 / mrcalc: [00;31m[WARNING] existing output files will be overwritten[0m |
| `bidsappmrtrix3connectome` | mredit voxel value | exit code 127, want 0 / mrconvert: [00;31m[WARNING] existing output files will be overwritten[0m |
| `bidsappmrtrix3connectome` | peaks2amp convert | exit code 127, want 0 / mrfilter: [00;31m[WARNING] existing output files will be overwritten[0m |
| `bidsappmrtrix3connectome` | transformcalc invert | exit code 127, want 0 / bash: line 3: transformcalc: command not found |
| `deepretinotopy` | FreeSurfer help | missing output fragment 'FreeSurfer' |
| `deepretinotopy` | FreeSurfer version | exit code 1, want 0 / Traceback (most recent call last): |
| `deepretinotopy` | bbregister available | missing output fragment 'bbregister' |
| `deepretinotopy` | mri_annotation2label available | missing output fragment 'mri_annotation2label' |
| `deepretinotopy` | mri_aparc2aseg available | missing output fragment 'mri_aparc2aseg' |
| `deepretinotopy` | mri_binarize available | missing output fragment 'mri_binarize' |
| `deepretinotopy` | mri_binarize threshold | exit code 1, want 0 / Traceback (most recent call last): |
| `deepretinotopy` | mri_ca_label available | missing output fragment 'mri_ca_label' |
| `deepretinotopy` | mri_ca_register available | missing output fragment 'mri_ca_register' |
| `deepretinotopy` | mri_concatenate_lta available | missing output fragment 'mri_concatenate_lta' |
| `deepretinotopy` | mri_convert available | missing output fragment 'mri_convert' |
| `deepretinotopy` | mri_convert format test | exit code 1, want 0 / Traceback (most recent call last): |
| `deepretinotopy` | mri_convert orientation | exit code 1, want 0 / Traceback (most recent call last): |
| `deepretinotopy` | mri_convert resample | exit code 1, want 0 / Traceback (most recent call last): |
| `deepretinotopy` | mri_coreg available | missing output fragment 'mri_coreg' |
| `deepretinotopy` | mri_em_register available | missing output fragment 'mri_em_register' |
| `deepretinotopy` | mri_fwhm available | missing output fragment 'mri_fwhm' |
| `deepretinotopy` | mri_glmfit available | missing output fragment 'mri_glmfit' |
| `deepretinotopy` | mri_info available | missing output fragment 'mri_info' |
| `deepretinotopy` | mri_info dimensions T1w | exit code 1, want 0 / Traceback (most recent call last): |
| `deepretinotopy` | mri_info on BOLD | missing output fragment 'voxel sizes' |
| `deepretinotopy` | mri_info on T1w | missing output fragment 'voxel sizes' |
| `deepretinotopy` | mri_info on T2 | missing output fragment 'voxel sizes' |
| `deepretinotopy` | mri_info voxel size T1w | exit code 1, want 0 / Traceback (most recent call last): |
| `deepretinotopy` | mri_label2vol available | missing output fragment 'mri_label2vol' |
| `deepretinotopy` | mri_robust_register available | missing output fragment 'mri_robust_register' |
| `deepretinotopy` | mri_segment available | missing output fragment 'mri_segment' |
| `deepretinotopy` | mri_segstats available | missing output fragment 'mri_segstats' |
| `deepretinotopy` | mri_surf2surf available | missing output fragment 'mri_surf2surf' |
| `deepretinotopy` | mri_surfcluster available | missing output fragment 'mri_surfcluster' |
| `deepretinotopy` | mri_synthseg available | missing output fragment 'SynthSeg' |
| `deepretinotopy` | mri_synthstrip available | missing output fragment 'SynthStrip' |
| `deepretinotopy` | mri_vol2surf available | missing output fragment 'mri_vol2surf' |
| `deepretinotopy` | mri_vol2vol available | missing output fragment 'mri_vol2vol' |
| `deepretinotopy` | mri_volcluster available | missing output fragment 'mri_volcluster' |
| `deepretinotopy` | mri_warp_convert available | missing output fragment 'mri_warp_convert' |
| `deepretinotopy` | mri_watershed available | missing output fragment 'mri_watershed' |
| `deepretinotopy` | mris_anatomical_stats available | missing output fragment 'mris_anatomical_stats' |
| `deepretinotopy` | mris_convert available | missing output fragment 'mris_convert' |
| `deepretinotopy` | mris_curvature available | missing output fragment 'mris_curvature' |
| `deepretinotopy` | mris_inflate available | missing output fragment 'mris_inflate' |
| `deepretinotopy` | mris_info available | missing output fragment 'mris_info' |
| `deepretinotopy` | mris_label2annot available | missing output fragment 'mris_label2annot' |
| `deepretinotopy` | mris_register available | missing output fragment 'mris_register' |
| `deepretinotopy` | mris_smooth available | missing output fragment 'mris_smooth' |
| `deepretinotopy` | mris_sphere available | missing output fragment 'mris_sphere' |
| `deepretinotopy` | recon-all available | missing output fragment 'recon-all' |
| `deepretinotopy` | recon-all stages | missing output fragment 'all' |
| `fmriprep` | FSL fslsplit temporal | exit code 127, want 0 / bash: line 2: fslsplit: command not found |
| `freesurfer` | AntsN4BiasFieldCorrectionFs basic | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | bbregister help | missing output fragment 'bbregister' |
| `freesurfer` | mri_WMHsynthseg help | missing output fragment 'WMH' |
| `freesurfer` | mri_aparc2aseg help | missing output fragment 'mri_aparc2aseg' |
| `freesurfer` | mri_binarize dilate | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_binarize erode | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_binarize invert | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_binarize percentage | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_binarize range | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_binarize threshold | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_ca_label help | missing output fragment 'mri_ca_label' |
| `freesurfer` | mri_concat max | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_concat mean | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_concat multiply | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_concat std | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_concat sum | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_convert NIfTI to MGZ | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_convert conform | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_convert crop | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_convert data type change | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_convert frame extraction | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_convert reorient | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_convert resample | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_coreg 12dof | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_coreg 9dof | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_coreg basic | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_diff identical | missing output fragment 'diffcount 0' |
| `freesurfer` | mri_em_register help | missing output fragment 'mri_em_register' |
| `freesurfer` | mri_info TR | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_info basic | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_info dimensions | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_info nframes | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_info orientation | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_info resolution | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_info vox2ras | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_nu_correct basic | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_robust_register affine | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_robust_register basic | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_robust_register with output | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_synthmorph help | missing output fragment 'synthmorph' |
| `freesurfer` | mri_synthsr help | missing output fragment 'synthsr' |
| `freesurfer` | mri_synthstrip basic | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_synthstrip border | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_synthstrip no csf | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_vol2vol cubic | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_vol2vol downsample | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_vol2vol nearest neighbor | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mri_vol2vol regheader | exit code 1, want 0 / Traceback (most recent call last): |
| `freesurfer` | mris_convert help | missing output fragment 'mris_convert' |
| `freesurfer` | recon-all version check | missing output fragment 'freesurfer' |
| `freesurfer` | samseg help | missing output fragment 'samseg' |
| `fsl` | fsl-cluster - find clusters | exit code 127, want 0 / bash: line 2: fsl-cluster: command not found |
| `fsl` | fsl_glm - basic regression | exit code 127, want 0 / bash: line 4: fsl_glm: command not found |
| `fsl` | fsl_regfilt - remove confounds | exit code 127, want 0 / bash: line 4: fsl_regfilt: command not found |
| `laynii` | LN2_CONNECTED_CLUSTERS labeling | exit code 127, want 0 / bash: line 3: LN2_CONNECTED_CLUSTERS: command not found |
| `laynii` | LN2_RIMIFY conversion | exit code 127, want 0 / bash: line 4: LN2_RIMIFY: command not found |
| `micapipe` | FreeSurfer mri_vol2vol identity | exit code 127, want 0 / bash: line 2: mri_vol2vol: command not found |
| `nibabies` | ANTs motion correction | exit code 127, want 0 / bash: line 3: antsMotionCorr: command not found |
| `qsiprep` | MRtrix3 dwi2fod help | exit code 127, want 0 / /opt/mrtrix3-latest/bin/dwi2fod |
| `qsiprep` | MRtrix3 mask filter - clean | exit code 127, want 0 / bash: line 3: maskfilter: command not found |
| `qsiprep` | MRtrix3 mask filter - connected components | exit code 127, want 0 / bash: line 3: maskfilter: command not found |
| `qsiprep` | MRtrix3 mask filter - dilate | exit code 127, want 0 / bash: line 3: maskfilter: command not found |
| `qsiprep` | MRtrix3 mask filter - erode | exit code 127, want 0 / bash: line 3: maskfilter: command not found |
| `qsiprep` | MRtrix3 mask filter - median | exit code 127, want 0 / bash: line 3: maskfilter: command not found |
| `qsiprep` | MRtrix3 tckgen help | exit code 127, want 0 / /opt/mrtrix3-latest/bin/tckgen |
| `rabies` | Bandpass filter simulation | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | Check multiprocessing | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | Compute DVARS | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | Compute framewise displacement | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | Error check utility help | missing output fragment 'Parser to handle testing' |
| `rabies` | Load NIfTI with nibabel | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | MELODIC availability | missing output fragment 'MELODIC' |
| `rabies` | RABIES analysis help | missing output fragment 'rabies analysis' |
| `rabies` | RABIES confound_correction help | missing output fragment 'rabies confound_correction' |
| `rabies` | RABIES help | missing output fragment 'RABIES performs multiple stages' |
| `rabies` | RABIES preprocess help | missing output fragment 'rabies preprocess' |
| `rabies` | Save as float32 | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | Simulate confound regression | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | antsMotionCorr on BOLD subset | exit code 127, want 0 / bash: line 3: fslroi: command not found |
| `rabies` | fMRI preprocessing chain | exit code 127, want 0 / bash: line 3: fslroi: command not found |
| `rabies` | fslmaths bandpass filter | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | fslmaths help | missing output fragment 'fslmaths' |
| `rabies` | fslmaths mean operation | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | fslmaths output types | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | fslmaths smoothing | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | fslmaths statistics | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | fslmaths threshold and binarize | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | matplotlib import | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nibabel import | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nilearn connectivity matrix | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nilearn import | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nilearn masking | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nilearn mean image | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nilearn resample to template | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nilearn smoothing | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nipype AFNI interface | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nipype ANTs interface | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nipype FSL interface | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nipype import | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | nipype plugin check | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | pandas import | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | rabies import | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | scipy import | exit code 1, want 0 / Traceback (most recent call last): |
| `rabies` | sklearn import | exit code 1, want 0 / Traceback (most recent call last): |
| `tractseg` | maskfilter clean | exit code 127, want 0 / bash: line 2: maskfilter: command not found |
| `tractseg` | maskfilter dilate | exit code 127, want 0 / bash: line 2: maskfilter: command not found |
| `tractseg` | maskfilter erode | exit code 127, want 0 / bash: line 2: maskfilter: command not found |
| `tractseg` | maskfilter median | exit code 127, want 0 / bash: line 2: maskfilter: command not found |

## Deploy/env variables not propagated

Tests: 31 across 11 suites.

Evidence: Tests that only check environment variables such as `FREESURFER_HOME`, `SPM_DIR`, MCR paths, or app roots fail under tinyrange.

Likely code surface: Projection of Neurodesk `DEPLOY_ENV_*`, image `.singularity.d/env`, and build metadata into runtime env.

### Affected Suites

| Suite | Tests |
| --- | ---: |
| `eeglab` | 6 |
| `bidsappspm` | 5 |
| `ospreybids` | 5 |
| `lashis` | 4 |
| `bidsapphcppipelines` | 3 |
| `brainager` | 2 |
| `fastsurfer` | 2 |
| `ashs` | 1 |
| `bidsappbaracus` | 1 |
| `diffusiontoolkit` | 1 |
| `samsrfx` | 1 |

### Representative Failures

| Suite | Test | Tinyrange symptom |
| --- | --- | --- |
| `ashs` | ASHS root environment | missing output fragment '/opt/ashs' |
| `bidsappbaracus` | FREESURFER_HOME check | missing output fragment '/opt/freesurfer' |
| `bidsapphcppipelines` | CARET7DIR set correctly | missing output fragment '/opt/workbench' |
| `bidsappspm` | LD_LIBRARY_PATH includes MCR | missing output fragment '/opt/mcr/v97' |
| `brainager` | Check LD_LIBRARY_PATH | missing output fragment '/opt/mcr' |
| `diffusiontoolkit` | Diffusion toolkit in PATH | missing output fragment '/opt/diffusiontoolkit-0.6.4.1' |
| `eeglab` | DEPLOY_BINS indicates EEGLAB | missing output fragment 'EEGLAB' |
| `fastsurfer` | FREESURFER_HOME set | missing output fragment '/opt/freesurfer' |

### Tests In Bucket

| Suite | Test | Symptom |
| --- | --- | --- |
| `ashs` | ASHS root environment | missing output fragment '/opt/ashs' |
| `bidsappbaracus` | FREESURFER_HOME check | missing output fragment '/opt/freesurfer' |
| `bidsapphcppipelines` | CARET7DIR set correctly | missing output fragment '/opt/workbench' |
| `bidsapphcppipelines` | FREESURFER_HOME set correctly | missing output fragment '/opt/freesurfer' |
| `bidsapphcppipelines` | HCPPIPEDIR set correctly | missing output fragment '/opt/HCP-Pipelines' |
| `bidsappspm` | LD_LIBRARY_PATH includes MCR | missing output fragment '/opt/mcr/v97' |
| `bidsappspm` | MCR_VERSION environment variable | missing output fragment 'v97' |
| `bidsappspm` | PATH includes SPM directory | missing output fragment '/opt/spm12' |
| `bidsappspm` | SPM_DIR environment variable | missing output fragment '/opt/spm12' |
| `bidsappspm` | SPM_EXEC environment variable | missing output fragment '/opt/spm12/spm12' |
| `brainager` | Check LD_LIBRARY_PATH | missing output fragment '/opt/mcr' |
| `brainager` | Check PATH includes brainageR | missing output fragment '/opt/brainageR' |
| `diffusiontoolkit` | Diffusion toolkit in PATH | missing output fragment '/opt/diffusiontoolkit-0.6.4.1' |
| `eeglab` | DEPLOY_BINS indicates EEGLAB | missing output fragment 'EEGLAB' |
| `eeglab` | LD_LIBRARY_PATH includes MCR bin | missing output fragment '/opt/MCR/v98/bin/glnxa64' |
| `eeglab` | LD_LIBRARY_PATH includes MCR runtime | missing output fragment '/opt/MCR/v98/runtime/glnxa64' |
| `eeglab` | LD_LIBRARY_PATH includes MCR sys/os | missing output fragment '/opt/MCR/v98/sys/os/glnxa64' |
| `eeglab` | PATH includes EEGLAB directory | missing output fragment '/opt/eeglab-2020.0/' |
| `eeglab` | XAPPLRESDIR set correctly | missing output fragment '/opt/MCR/v98' |
| `fastsurfer` | FREESURFER_HOME set | missing output fragment '/opt/freesurfer' |
| `fastsurfer` | FS_LICENSE environment variable | missing output fragment '/opt/license.txt' |
| `lashis` | ANTSPATH environment variable | missing output fragment '/opt/ants-2.3.0' |
| `lashis` | ASHS_ROOT environment variable | missing output fragment '/opt/ashs-2.0.0' |
| `lashis` | PATH includes ANTs | missing output fragment '/opt/ants-2.3.0' |
| `lashis` | PATH includes ASHS | missing output fragment '/opt/ashs-2.0.0/bin' |
| `ospreybids` | BASIS_SETS_PATH environment variable | missing output fragment '/HBCD_basissets' |
| `ospreybids` | DEPLOY_BINS environment variable | missing output fragment 'osprey' |
| `ospreybids` | EXECUTABLE_PATH environment variable | missing output fragment '/code/run_compiled.sh' |
| `ospreybids` | LD_LIBRARY_PATH includes MCR | missing output fragment '/mcr_path/v912' |
| `ospreybids` | MCR_PATH environment variable | missing output fragment '/mcr_path/v912' |
| `samsrfx` | PATH includes samsrfx | missing output fragment '/opt/samsrfx-v10.004/' |

## FSL environment not initialized

Tests: 46 across 1 suites.

Evidence: FSL commands are present enough for availability checks, but FSL scripts run without `FSLDIR`/`FSLOUTPUTTYPE` and call helpers as `/bin/...`.

Likely code surface: FSL-specific activation from Singularity env files and `DEPLOY_ENV_*` handling; likely same root as broader deploy env propagation.

### Affected Suites

| Suite | Tests |
| --- | ---: |
| `bidsappaa` | 46 |

### Representative Failures

| Suite | Test | Tinyrange symptom |
| --- | --- | --- |
| `bidsappaa` | FSL bet basic brain extraction | missing output /home/runner/work/_temp/pyneurodesk-fulltest-bidsappaa/test_output/fsl/struct_brain.nii.gz |

### Tests In Bucket

| Suite | Test | Symptom |
| --- | --- | --- |
| `bidsappaa` | FSL bet basic brain extraction | missing output /home/runner/work/_temp/pyneurodesk-fulltest-bidsappaa/test_output/fsl/struct_brain.nii.gz |
| `bidsappaa` | FSL bet on T2 image | missing output /home/runner/work/_temp/pyneurodesk-fulltest-bidsappaa/test_output/fsl/t2_brain.nii.gz |
| `bidsappaa` | FSL flirt affine registration | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL flirt mutual information cost | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL flirt rigid registration | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL flirt schedule file | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslhd BOLD header | missing output fragment 'dim4' |
| `bidsappaa` | FSL fslhd display header | missing output fragment 'sizeof_hdr' |
| `bidsappaa` | FSL fslinfo on BOLD | exit code 1, want 0 / /opt/fsl/bin/fslinfo: 77: /opt/fsl/bin/fslinfo: /bin/fslhd: not found |
| `bidsappaa` | FSL fslinfo on structural | exit code 1, want 0 / /opt/fsl/bin/fslinfo: 77: /opt/fsl/bin/fslinfo: /bin/fslhd: not found |
| `bidsappaa` | FSL fslmaths absolute value | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths add constant | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths binarize | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths divide constant | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths multiply constant | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths reciprocal | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths smooth | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths square | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths square root | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths subtract constant | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths temporal max | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths temporal mean | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths temporal min | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths temporal std | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslmaths threshold | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslroi spatial crop | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslroi temporal subset | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslsplit temporal | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslstats basic | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslstats center of gravity | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslstats histogram | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslstats mean | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslstats percentile | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslstats robust range | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL fslstats volume | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL mcflirt basic motion correction | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL mcflirt with mean volume reference | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL mcflirt with statistics | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL mcflirt with transformation matrices | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | FSL susan smoothing | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | Pipeline brain mask and stats | exit code 1, want 0 / /opt/fsl/bin/bet: 1: /opt/fsl/bin/bet: /bin/remove_ext: not found |
| `bidsappaa` | Pipeline coregistration | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | Pipeline motion correction with QC | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | Pipeline smoothing comparison | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | Pipeline tSNR calculation | exit code 1, want 0 / ERROR:: Environment variable FSLOUTPUTTYPE is not set! |
| `bidsappaa` | Pipeline tissue segmentation | exit code 1, want 0 / /opt/fsl/bin/bet: 1: /opt/fsl/bin/bet: /bin/remove_ext: not found |

## Install-root path collapsed to /../...

Tests: 47 across 1 suites.

Evidence: Package-relative paths lost their install-root prefix, producing paths such as `/../ashs-fastashs_beta/...`.

Likely code surface: Singularity/env path parsing and BASEPATH/deploy metadata normalization in `pyneurodesk/src/pyneurodesk/api.py`.

### Affected Suites

| Suite | Tests |
| --- | ---: |
| `ashs` | 47 |

### Representative Failures

| Suite | Test | Tinyrange symptom |
| --- | --- | --- |
| `ashs` | ANTs AverageImages run | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/ants_1042/AverageImages: No such file or directory |

### Tests In Bucket

| Suite | Test | Symptom |
| --- | --- | --- |
| `ashs` | ANTs AverageImages run | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/ants_1042/AverageImages: No such file or directory |
| `ashs` | ANTs ImageMath gradient | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/ants_1042/ImageMath: No such file or directory |
| `ashs` | ANTs ImageMath normalize | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/ants_1042/ImageMath: No such file or directory |
| `ashs` | ANTs MeasureMinMaxMean run | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/ants_1042/MeasureMinMaxMean: No such file or directory |
| `ashs` | ANTs MultiplyImages run | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/ants_1042/MultiplyImages: No such file or directory |
| `ashs` | ANTs N3BiasFieldCorrection run | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/ants_1042/N3BiasFieldCorrection: No such file or directory |
| `ashs` | ANTs ThresholdImage run | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/ants_1042/ThresholdImage: No such file or directory |
| `ashs` | C3D add constant | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D affine create rotation | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d_affine_tool: No such file or directory |
| `ashs` | C3D affine create scaling | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d_affine_tool: No such file or directory |
| `ashs` | C3D affine export ITK | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d_affine_tool: No such file or directory |
| `ashs` | C3D affine from sform | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d_affine_tool: No such file or directory |
| `ashs` | C3D affine info | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d_affine_tool: No such file or directory |
| `ashs` | C3D affine inverse | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d_affine_tool: No such file or directory |
| `ashs` | C3D clip | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D exp | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D flip | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D image info | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D image info full | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D log | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D multiply | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D orient | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D pad | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D resample | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D reslice identity | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D scale | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D smooth | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D sqrt | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D stretch | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D threshold | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D version | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | C3D voxel sum | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | FSL bet2 run | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/fsl/bet2: No such file or directory |
| `ashs` | FSL flirt run | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/fsl/flirt: No such file or directory |
| `ashs` | Greedy affine registration | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/greedy: No such file or directory |
| `ashs` | Greedy deformable registration | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/greedy: No such file or directory |
| `ashs` | Greedy moments initialization | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/greedy: No such file or directory |
| `ashs` | Greedy rigid registration | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/greedy: No such file or directory |
| `ashs` | Greedy version | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/greedy: No such file or directory |
| `ashs` | Label fusion version | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/label_fusion: No such file or directory |
| `ashs` | NLMDenoise Rician | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/NLMDenoise: No such file or directory |
| `ashs` | NLMDenoise run | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/NLMDenoise: No such file or directory |
| `ashs` | NLMUpsample run | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/NLMUpsample: No such file or directory |
| `ashs` | Pipeline brain extraction and masking | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/fsl/bet2: No such file or directory |
| `ashs` | Pipeline morphology operations | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/c3d: No such file or directory |
| `ashs` | Pipeline preprocessing | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/ants_1042/N3BiasFieldCorrection: No such file or directory |
| `ashs` | Pipeline registration chain | exit code 127, want 0 / bash: line 2: /../ashs-fastashs_beta/ext/Linux/bin/greedy: No such file or directory |

## Python package/import environment missing

Tests: 89 across 2 suites.

Evidence: The Python executable runs, but package imports that work in Apptainer are missing in tinyrange.

Likely code surface: Container environment activation, Python/conda PATH and PYTHONPATH propagation, and deploy env parsing.

### Affected Suites

| Suite | Tests |
| --- | ---: |
| `sovabids` | 87 |
| `batchheudiconv` | 2 |

### Representative Failures

| Suite | Test | Tinyrange symptom |
| --- | --- | --- |
| `batchheudiconv` | Multi-file NIfTI inspection | exit code 1, want 0 / === T2 === |
| `sovabids` | Check MNE Raw object creation | exit code 1, want 0 / Traceback (most recent call last): |

### Tests In Bucket

| Suite | Test | Symptom |
| --- | --- | --- |
| `batchheudiconv` | Multi-file NIfTI inspection | exit code 1, want 0 / === T2 === |
| `batchheudiconv` | nib-ls BOLD with stats | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check MNE Raw object creation | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check MNE installation | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check MNE-BIDS installation | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check NULL values constant | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check PyYAML installation | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check SECTION_STRING constant | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check bids-validator installation | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check numpy installation | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check pandas installation | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check pybv installation | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check scipy installation | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check sovabids module import | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check sovabids package location | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check sovabids version | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Check supported extensions | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Create BIDSValidator instance | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Create dummy raw and check channels | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Create dummy raw and check duration | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Create dummy raw and check sampling frequency | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import ApplyError exception | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import BIDSPath from MNE-BIDS | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import BIDSValidator class | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import ConvertError exception | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import FileListError exception | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import RulesError exception | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import SaveError exception | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import apply_rules function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import apply_rules_to_single_file | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import bids module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import convert module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import convert_them function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import datasets module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import deep_get function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import deep_merge function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import deep_merge_N function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import dicts module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import errors module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import files module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import find_bidsroot function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import flat_paren_counter function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import flatten function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import from_io_example function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import get_dummy_raw function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import get_files function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import get_num_digits function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import get_sova2coin_bidsmap | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import heuristics module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import load_rules function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import loggers module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import make_dummy_dataset function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import misc module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import nested_notation_to_tree | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import parse_entities_from_bidspath | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import parse_from_placeholder | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import parse_from_regex | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import parse_path_pattern_from_entities | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import parsers module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import placeholder_to_regex | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import rules module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import schemas module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import settings module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import setup_logging function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import sovaconvert function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import sovarpc module | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import update_dataset_description | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Import write_raw_bids from MNE-BIDS | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Supported extensions include BDF | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Supported extensions include CNT | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Supported extensions include EDF | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Supported extensions include FIF | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Supported extensions include SET | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test BIDSValidator is_bids method | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test deep_get with path | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test deep_merge with nested dicts | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test from_io_example basic usage | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test full from_io_example workflow | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test get_dummy_raw function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test get_num_digits function | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test load_rules with dictionary | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test nested_notation_to_tree | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test parse_entities with acquisition | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test parse_entities with run | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test parse_entities_from_bidspath | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test parse_from_placeholder | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | Test placeholder_to_regex conversion | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | sovaconvert help | exit code 1, want 0 / Traceback (most recent call last): |
| `sovabids` | sovapply help | exit code 1, want 0 / Traceback (most recent call last): |

## Workdir/share or generated fixture visibility

Tests: 123 across 6 suites.

Evidence: Tests create or expect files under the fulltest work directory, but the command in tinyrange cannot see or write the same paths Apptainer sees.

Likely code surface: Implicit share/workdir mounting from `pyneurodesk/src/pyneurodesk/shell.py` into the VM backend and hostfs path mapping.

### Affected Suites

| Suite | Tests |
| --- | ---: |
| `trackvis` | 72 |
| `palm` | 40 |
| `networkcorrespondancetoolkit` | 5 |
| `mritools` | 3 |
| `root` | 2 |
| `cat12` | 1 |

### Representative Failures

| Suite | Test | Tinyrange symptom |
| --- | --- | --- |
| `cat12` | Run SPM12 deface | missing output /home/runner/work/_temp/pyneurodesk-fulltest-cat12/test_output/anon_sub-01_T1w.nii |
| `mritools` | Create 4D synthetic phase from BOLD | exit code 2, want 0 / ls: cannot access 'test_output/synthetic/phase_4d_mean.nii': No such file or directory |
| `networkcorrespondancetoolkit` | Handle missing file gracefully | missing output fragment 'Correctly raised' |
| `palm` | Basic input loading | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `root` | Save histogram to PNG | missing output /home/runner/work/_temp/pyneurodesk-fulltest-root/test_output/hist.png |
| `trackvis` | Alternative color coding | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |

### Tests In Bucket

| Suite | Test | Symptom |
| --- | --- | --- |
| `cat12` | Run SPM12 deface | missing output /home/runner/work/_temp/pyneurodesk-fulltest-cat12/test_output/anon_sub-01_T1w.nii |
| `mritools` | Create 4D synthetic phase from BOLD | exit code 2, want 0 / ls: cannot access 'test_output/synthetic/phase_4d_mean.nii': No such file or directory |
| `mritools` | Create synthetic magnitude for testing | exit code 2, want 0 / ls: cannot access 'test_output/synthetic/mag_3d.nii': No such file or directory |
| `mritools` | Create synthetic phase image from T1w | exit code 2, want 0 / ls: cannot access 'test_output/synthetic/phase_3d.nii': No such file or directory |
| `networkcorrespondancetoolkit` | Handle missing file gracefully | missing output fragment 'Correctly raised' |
| `networkcorrespondancetoolkit` | Large array handling | exit code 255, want 0 |
| `networkcorrespondancetoolkit` | Pandas DataFrame operations | missing output /home/runner/work/_temp/pyneurodesk-fulltest-networkcorrespondancetoolkit/test_output/network_results.csv |
| `networkcorrespondancetoolkit` | Scipy statistical functions | missing output fragment 'Skewness:' |
| `networkcorrespondancetoolkit` | Sklearn PCA | exit code 255, want 0 |
| `palm` | Basic input loading | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Beckmann partition method | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Combined EE and ISE | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Dekker method | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Double precision | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Exhaustive permutations | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | F-test | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | F-test only | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | FDR correction | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Fixed seed for reproducibility | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Freedman-Lane method | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Gamma approximation acceleration | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Guttman partition method | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Inverse normal Blom method | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Inverse normal transformation | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Log p-values | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Multiple inputs | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Negative binomial acceleration | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | No permutation approximation | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | One-sample t-test | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | One-sample with save1-p | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Quiet mode | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Save 1-p values | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Save DOF | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Save GLM outputs | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Save effective mask | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Save maximum statistic distribution | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Save parametric p-values | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Save permutation metrics | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Sign-flipping test | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Synchronized permutations | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Tail approximation acceleration | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Two-sample t-test (unpaired) | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Two-sample with auto variance groups | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Two-sample with variance groups | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Two-tailed t-test | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Verbose filenames | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Whole-block permutation | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | Within-block permutation | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `palm` | ter Braak method | exit code 1, want 0 / octave: X11 DISPLAY environment variable not set |
| `root` | Save histogram to PNG | missing output /home/runner/work/_temp/pyneurodesk-fulltest-root/test_output/hist.png |
| `root` | hadd merge files | missing output /home/runner/work/_temp/pyneurodesk-fulltest-root/test_output/merged.root |
| `trackvis` | Alternative color coding | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Anti-aliasing | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Axial brain slice | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Axial slice filter | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Axis thickness | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Background color | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Background tracks | exit code 1, want 0 / Can not open file test_output/synthetic_tracks2.trk |
| `trackvis` | Ball marker | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Both ends ROI filter | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Brain image overlay | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Camera azimuth rotation | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Camera dolly (zoom) | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Camera elevation rotation | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Camera offset | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Circle display | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Complex filter pipeline | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Coronal brain slice | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Coronal slice filter | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Curvature threshold | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Custom log ID | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Directional color coding | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Disable log | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Display track count | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Dual ROI files | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Dual ROI pointers | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Duplicate arguments | exit code 1, want 0 / Can not open file test_output/synthetic_tracks2.trk |
| `trackvis` | End ROI filter | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | End point iteration | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | End point sagittal filter | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Extend tracks | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Filter with ROI and output | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Filter with brain overlay | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Frame box display | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Full display pipeline | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Helix color coding | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | KT threshold | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Lattice ROI filter | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Length threshold (minimum only) | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Length threshold (range) | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Length threshold output | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Load track file (no render) | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | No annotation | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Number of sides | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Output ROI volume | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Output endpoint volume | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Output filtered tracks | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Output track volume | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Proportional skip | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | ROI disk filter | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | ROI display with tracks | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | ROI from NIfTI file | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | ROI pointer (single voxel) | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | ROI tube filter | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Sagittal brain slice | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Sagittal slice exclusion | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Sagittal slice filter | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Save camera position | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Screen capture magnification | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Shading option | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Skip tracks | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Slab filter (multiple slices) | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Solid color option | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Surface file generation | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Surface threshold | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Title overlay | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Torsion threshold | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Transparent display | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Tube radius | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | U-factor threshold | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Window level adjustment | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Window size | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |
| `trackvis` | Wireframe option | exit code 1, want 0 / Can not open file test_output/synthetic_tracks.trk |

## Timeouts / slow execution under tinyrange

Tests: 188 across 32 suites.

Evidence: Commands that complete under Apptainer exceed the tinyrange fulltest timeout, or the suite is dominated by tests that hit the same slow path.

Likely code surface: VM/runtime performance, process startup overhead, I/O throughput, CPU allocation, and `pyneurodesk/src/pyneurodesk/fulltest.py` timeout/resource defaults.

### Affected Suites

| Suite | Tests |
| --- | ---: |
| `hmri` | 33 |
| `qupath` | 33 |
| `ilastik` | 17 |
| `spm25` | 17 |
| `mgltools` | 14 |
| `qsmxt` | 8 |
| `micapipe` | 7 |
| `mrsiproc` | 7 |
| `neurodock` | 6 |
| `sigviewer` | 6 |
| `ants` | 5 |
| `qsirecon` | 5 |
| `dcm2bids` | 4 |
| `dsistudio` | 3 |
| `networkcorrespondancetoolkit` | 3 |
| `rstudio` | 3 |
| `mriqc` | 2 |
| `batchheudiconv` | 1 |
| `bidsappspm` | 1 |
| `bidscoin` | 1 |
| `cat12` | 1 |
| `ezbids` | 1 |
| `fmriprep` | 1 |
| `fsl` | 1 |
| `gigaconnectome` | 1 |
| `nftsim` | 1 |
| `nibabies` | 1 |
| `osprey` | 1 |
| `ospreybids` | 1 |
| `palmettobug` | 1 |
| `root` | 1 |
| `slicer` | 1 |

### Representative Failures

| Suite | Test | Tinyrange symptom |
| --- | --- | --- |
| `ants` | Affine initializer | exit code 124, want 0 / bad det -1 v 1 u -1 |
| `batchheudiconv` | Create study directory structure | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-batchheudiconv/.pyneurodesk-fulltest-activate.sh |
| `bidsappspm` | Participant preprocessing - single subject | exit code 124, want 0 / SPM12, version 7771 (standalone) |
| `bidscoin` | BIDScoin version check | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-bidscoin/.pyneurodesk-fulltest-activate.sh |
| `cat12` | Run CAT12 segmentation (no surface) | exit code 124, want 0 / ------------------------------------------ |
| `dcm2bids` | Create mock NIfTI with JSON sidecar (BOLD) | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-dcm2bids/.pyneurodesk-fulltest-activate.sh |
| `dsistudio` | atk action - optic radiation | exit code 124, want 0 / DSI Studio version: Hou "侯" Oct 10 2024 |
| `ezbids` | pm2 version | exit code 124, want 0 / ------------- |

### Tests In Bucket

| Suite | Test | Symptom |
| --- | --- | --- |
| `ants` | Affine initializer | exit code 124, want 0 / bad det -1 v 1 u -1 |
| `ants` | Denoise image Gaussian model | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ants/.pyneurodesk-fulltest-activate.sh |
| `ants` | Denoise image Rician model | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ants/.pyneurodesk-fulltest-activate.sh |
| `ants` | Denoise with noise image output | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ants/.pyneurodesk-fulltest-activate.sh |
| `ants` | N4 with custom convergence | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ants/.pyneurodesk-fulltest-activate.sh |
| `batchheudiconv` | Create study directory structure | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-batchheudiconv/.pyneurodesk-fulltest-act... |
| `bidsappspm` | Participant preprocessing - single subject | exit code 124, want 0 / SPM12, version 7771 (standalone) |
| `bidscoin` | BIDScoin version check | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-bidscoin/.pyneurodesk-fulltest-activate.... |
| `cat12` | Run CAT12 segmentation (no surface) | exit code 124, want 0 / ------------------------------------------ |
| `dcm2bids` | Create mock NIfTI with JSON sidecar (BOLD) | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-dcm2bids/.pyneurodesk-fulltest-activate.... |
| `dcm2bids` | Create mock NIfTI with JSON sidecar (T1w) | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-dcm2bids/.pyneurodesk-fulltest-activate.... |
| `dcm2bids` | Create mock T2w data | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-dcm2bids/.pyneurodesk-fulltest-activate.... |
| `dcm2bids` | dcm2niix BIDS sidecar only mode | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-dcm2bids/.pyneurodesk-fulltest-activate.... |
| `dsistudio` | atk action - optic radiation | exit code 124, want 0 / DSI Studio version: Hou "侯" Oct 10 2024 |
| `dsistudio` | atk action - track corticospinal tract | exit code 124, want 0 / DSI Studio version: Hou "侯" Oct 10 2024 |
| `dsistudio` | atk action - track multiple bundles | exit code 124, want 0 / DSI Studio version: Hou "侯" Oct 10 2024 |
| `ezbids` | pm2 version | exit code 124, want 0 / ------------- |
| `fmriprep` | DenoiseImage on T1w | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-fmriprep/.pyneurodesk-fulltest-activate.... |
| `fsl` | fslcpgeom - copy geometry | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-fsl/.pyneurodesk-fulltest-activate.sh |
| `gigaconnectome` | pybids layout creation | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-gigaconnectome/.pyneurodesk-fulltest-act... |
| `hmri` | MATLAB runtime version | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | SPM directory location | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | SPM version string | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | SPM12 help display | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | SPM12 version information | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hMRI B1 defaults availability | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hMRI defaults availability | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hMRI get_defaults function | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-hmri/.pyneurodesk-fulltest-activate.sh |
| `hmri` | hMRI toolbox version | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_autoreorient function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_calc_A function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_calc_R1 function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_calc_R2s function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_coreg function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_create_MTProt function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_create_b1map function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_create_nifti function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_create_unicort function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_log function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_proc_MPMsmooth function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_quality_display function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_quiqi_build function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_quiqi_check function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_read_vols function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_run_proc_US function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | hmri_run_proc_dartel_norm function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | spm_read_vols function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | spm_smooth function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | spm_vol function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | spm_write_vol function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | tbx_cfg_hmri function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | tbx_scfg_hmri_B1_create function exists | exit code 124, want 0 / ------------------------------------------ |
| `hmri` | tbx_scfg_hmri_create function exists | exit code 124, want 0 / ------------------------------------------ |
| `ilastik` | Headless mode without project error | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help message | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows configfile option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows debug option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows exit_on_failure option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows headless option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows logfile option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows neural network device option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows new_project option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows project option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows readonly option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows redirect_output option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows tiktorch option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Help shows workflow option | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Missing project file error | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | New project requires workflow | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `ilastik` | Version check | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh |
| `mgltools` | compute_interatomic_distance_per_pose.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | compute_interatomic_distance_per_vina_pose.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | compute_rms_between_conformations.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | mglobabel available | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.... |
| `mgltools` | process_VinaResult.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | summarize_docking.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | summarize_docking.py rmsd option | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | summarize_docking_directory.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | summarize_results4.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | summarize_results41.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | write_all_complexes.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | write_clustering_histogram_postscript.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | write_conformations_from_dlg.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `mgltools` | write_lowest_energy_ligand.py help | exit code 124, want 0 / setting PYTHONHOME environment |
| `micapipe` | Workbench list commands | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.... |
| `micapipe` | Workbench version | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.... |
| `micapipe` | Workbench volume dilate | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.... |
| `micapipe` | Workbench volume info | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.... |
| `micapipe` | Workbench volume math | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.... |
| `micapipe` | Workbench volume resample | exit code 124, want 255 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activat... |
| `micapipe` | Workbench volume smoothing | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.... |
| `mriqc` | Create BIDS layout database | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-mriqc/.pyneurodesk-fulltest-activate.sh |
| `mriqc` | Nilearn extract time series | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-mriqc/.pyneurodesk-fulltest-activate.sh |
| `mrsiproc` | CreateSpectralNiftiMap binary exists | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.... |
| `mrsiproc` | GetPar_CreateTempl_MaskPart1 binary exists | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.... |
| `mrsiproc` | MRSI_Reconstruction responds without input | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.... |
| `mrsiproc` | extract_met_maps binary exists | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.... |
| `mrsiproc` | extract_spectra binary exists | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.... |
| `mrsiproc` | julia_write_lcm_files binary exists | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.... |
| `mrsiproc` | segmentation_simple binary exists | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.... |
| `networkcorrespondancetoolkit` | NCT comprehensive integration test | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-networkcorrespondancetoolkit/.pyneurodes... |
| `networkcorrespondancetoolkit` | Numpy save array | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-networkcorrespondancetoolkit/.pyneurodes... |
| `networkcorrespondancetoolkit` | Scipy ndimage operations | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-networkcorrespondancetoolkit/.pyneurodes... |
| `neurodock` | DIPY dipy_buan_lmm help | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate... |
| `neurodock` | DIPY dipy_buan_profiles help | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate... |
| `neurodock` | DIPY dipy_buan_shapes help | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate... |
| `neurodock` | PyDesigner advanced options | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate... |
| `neurodock` | PyDesigner help | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate... |
| `neurodock` | PyDesigner version check | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate... |
| `nftsim` | rTMS plasticity | exit code 124, want 0 / WARNING: Value of total simulation Time not divisible by Deltat. |
| `nibabies` | ANTs denoise image | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-nibabies/.pyneurodesk-fulltest-activate.... |
| `osprey` | ospreyCMD shows help (no args) | exit code 124, want 0 / ------------------------------------------ |
| `ospreybids` | OspreyHBCD job file error | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-ospreybids/.pyneurodesk-fulltest-activat... |
| `palmettobug` | Dcor import and version | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-palmettobug/.pyneurodesk-fulltest-activa... |
| `qsirecon` | Affine initializer | exit code 124, want 0 / bad det -1 v 1 u -1 |
| `qsirecon` | Denoise image Gaussian model | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qsirecon/.pyneurodesk-fulltest-activate.... |
| `qsirecon` | Denoise image Rician model | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qsirecon/.pyneurodesk-fulltest-activate.... |
| `qsirecon` | Pipeline N4 then denoise | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qsirecon/.pyneurodesk-fulltest-activate.... |
| `qsirecon` | Pipeline preprocessing chain | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qsirecon/.pyneurodesk-fulltest-activate.... |
| `qsmxt` | DenoiseImage basic | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh |
| `qsmxt` | DenoiseImage with noise image | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh |
| `qsmxt` | Julia CLEARSWI package | exit code 124, want 0 / ┌ Warning: The call to compilecache failed to create a usable precompiled cache file for CodecZlib [944b1d66-785c-5afd-91f1-9de20f53319... |
| `qsmxt` | Julia MriResearchTools package | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh |
| `qsmxt` | Julia QuantitativeSusceptibilityMappingTGV package | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh |
| `qsmxt` | Julia ROMEO package | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh |
| `qsmxt` | Multi-modal registration pipeline | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh |
| `qsmxt` | T1w preprocessing pipeline | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh |
| `qupath` | Access ColorTools class | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qupath/.pyneurodesk-fulltest-activate.sh |
| `qupath` | Access GeneralTools class | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Access GeoJSON export | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qupath/.pyneurodesk-fulltest-activate.sh |
| `qupath` | Access ImageData class | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Access PathClassFactory | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Access PathObjects class | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Access ROI classes | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Access measurement classes | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Create RGB color | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Create and run computation script | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Create and run simple script file | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Create color with alpha | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qupath/.pyneurodesk-fulltest-activate.sh |
| `qupath` | Create derived path class | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Create ellipse ROI | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Create line ROI | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Create path class | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qupath/.pyneurodesk-fulltest-activate.sh |
| `qupath` | Create point ROI | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Create rectangle ROI | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Extract color components | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qupath/.pyneurodesk-fulltest-activate.sh |
| `qupath` | ROI bounding box | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qupath/.pyneurodesk-fulltest-activate.sh |
| `qupath` | Script inline arithmetic | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Script inline class access | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Script inline closure | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qupath/.pyneurodesk-fulltest-activate.sh |
| `qupath` | Script inline list creation | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qupath/.pyneurodesk-fulltest-activate.sh |
| `qupath` | Script inline loop | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-qupath/.pyneurodesk-fulltest-activate.sh |
| `qupath` | Script inline map creation | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Script inline multiline | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Script inline print statement | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Script inline string manipulation | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Script inline version access | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Script with arguments | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Script with quoted argument list | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `qupath` | Strip file extension | exit code 124, want 0 / WARNING: Unknown module: javafx.graphics specified to --add-opens |
| `root` | rootcp copy object | exit code 124, want 0 / Error in <TFile::ReadBuffer>: error reading all requested bytes from file test_output/test_hist.root, got 63 of 300 |
| `rstudio` | PDF graphics output | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-rstudio/.pyneurodesk-fulltest-activate.sh |
| `rstudio` | PNG graphics output | exit code 124, want 0 / [fulltest] command timed out after 300.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-rstudio/.pyneurodesk-fulltest-activate.sh |
| `rstudio` | ggplot2 save | exit code 124, want 0 / [fulltest] command timed out after 300.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-rstudio/.pyneurodesk-fulltest-activate.sh |
| `sigviewer` | Directory as input | exit code 124, want 0 / QStandardPaths: XDG_RUNTIME_DIR not set, defaulting to '/tmp/runtime-root' |
| `sigviewer` | Empty filename handling | exit code 124, want 0 / QStandardPaths: XDG_RUNTIME_DIR not set, defaulting to '/tmp/runtime-root' |
| `sigviewer` | Invalid file format handling | exit code 124, want 0 / QStandardPaths: XDG_RUNTIME_DIR not set, defaulting to '/tmp/runtime-root' |
| `sigviewer` | Nonexistent file handling | exit code 124, want 0 / QStandardPaths: XDG_RUNTIME_DIR not set, defaulting to '/tmp/runtime-root' |
| `sigviewer` | Offscreen startup test | exit code 124, want 0 / QStandardPaths: XDG_RUNTIME_DIR not set, defaulting to '/tmp/runtime-root' |
| `sigviewer` | Permission denied handling | exit code 124, want 0 / QStandardPaths: XDG_RUNTIME_DIR not set, defaulting to '/tmp/runtime-root' |
| `slicer` | Slicer launcher help | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-slicer/.pyneurodesk-fulltest-activate.sh |
| `spm25` | ImCalc absolute value | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc binarize | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc division | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc exponential | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc help | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc identity operation | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc log transform | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc min clamp | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc power function | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc scalar addition | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc scalar multiplication | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc square root | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc thresholding | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc trigonometric cos | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | ImCalc trigonometric sin | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | SPM25 help | exit code 124, want 0 / ------------------------------------------ |
| `spm25` | SPM25 verbose help | exit code 124, want 0 / [fulltest] command timed out after 120.0s: source /home/runner/work/_temp/pyneurodesk-fulltest-spm25/.pyneurodesk-fulltest-activate.sh |

## Process killed / memory pressure

Tests: 5 across 3 suites.

Evidence: Commands are killed rather than returning a normal failure; Apptainer completes the same tests.

Likely code surface: VM memory sizing, cgroup/oom behavior, and fulltest `--memory-mb` defaults.

### Affected Suites

| Suite | Tests |
| --- | ---: |
| `ezbids` | 2 |
| `fsl` | 2 |
| `bidsappspm` | 1 |

### Representative Failures

| Suite | Test | Tinyrange symptom |
| --- | --- | --- |
| `bidsappspm` | Multi-subject preprocessing | exit code 137, want 0 / SPM12, version 7771 (standalone) |
| `ezbids` | Brain extraction and defacing pipeline | exit code 137, want 0 / Step 1 of 9: reading in images...Done! It took roughly 0 seconds |
| `fsl` | fsl_motion_outliers - DVARS | exit code 137, want 0 / Killed |

### Tests In Bucket

| Suite | Test | Symptom |
| --- | --- | --- |
| `bidsappspm` | Multi-subject preprocessing | exit code 137, want 0 / SPM12, version 7771 (standalone) |
| `ezbids` | Brain extraction and defacing pipeline | exit code 137, want 0 / Step 1 of 9: reading in images...Done! It took roughly 0 seconds |
| `ezbids` | Complete T1w processing pipeline | exit code 137, want 0 / Step 1 of 9: reading in images...Done! It took roughly 0 seconds |
| `fsl` | fsl_motion_outliers - DVARS | exit code 137, want 0 / Killed |
| `fsl` | fsl_motion_outliers - FD | exit code 137, want 0 / Killed |

## Executable format / binfmt handling

Tests: 1 across 1 suites.

Evidence: The VM tries to execute a file but receives `exec format error`.

Likely code surface: Architecture selection, binfmt/qemu handling, or an invalid command entry being exposed as executable.

### Affected Suites

| Suite | Tests |
| --- | ---: |
| `bidsappbaracus` | 1 |

### Representative Failures

| Suite | Test | Tinyrange symptom |
| --- | --- | --- |
| `bidsappbaracus` | FreeSurfer version check | exit code 126, want 0 / ccx3-init: exec error: fork/exec /opt/freesurfer/bin/freesurfer: exec format error |

### Tests In Bucket

| Suite | Test | Symptom |
| --- | --- | --- |
| `bidsappbaracus` | FreeSurfer version check | exit code 126, want 0 / ccx3-init: exec error: fork/exec /opt/freesurfer/bin/freesurfer: exec format error |

## Command behavior/output mismatch after launch

Tests: 142 across 36 suites.

Evidence: The command starts but returns different output, a different nonzero status, or different CLI semantics under tinyrange.

Likely code surface: Usually downstream of missing env/workdir setup; after the larger buckets are fixed, re-run these to separate real runtime behavior differences from cascades.

### Affected Suites

| Suite | Tests |
| --- | ---: |
| `nftsim` | 19 |
| `root` | 10 |
| `ashs` | 7 |
| `batchheudiconv` | 7 |
| `cat12` | 7 |
| `glmsingle` | 7 |
| `micapipe` | 7 |
| `romeo` | 7 |
| `code` | 6 |
| `sigviewer` | 6 |
| `fsl` | 5 |
| `minc` | 5 |
| `nibabies` | 5 |
| `lcmodel` | 4 |
| `mriqc` | 4 |
| `aidamri` | 3 |
| `bidscoin` | 3 |
| `spm12` | 3 |
| `bidsappmrtrix3connectome` | 2 |
| `brainager` | 2 |
| `gigaconnectome` | 2 |
| `lesymap` | 2 |
| `linda` | 2 |
| `osprey` | 2 |
| `qsiprep` | 2 |
| `qsmxt` | 2 |
| `sovabids` | 2 |
| `bidsappaa` | 1 |
| `bidsappbaracus` | 1 |
| `brainnetviewer` | 1 |
| `dicompare` | 1 |
| `fastsurfer` | 1 |
| `laynii` | 1 |
| `mricron` | 1 |
| `mrsiproc` | 1 |
| `qmrlab` | 1 |

### Representative Failures

| Suite | Test | Tinyrange symptom |
| --- | --- | --- |
| `aidamri` | Nipype BET interface | exit code 1, want 0 / Traceback (most recent call last): |
| `ashs` | ASHS main help | missing output fragment 'ashs_main: automatic segmentation of hippocampal subfields' |
| `batchheudiconv` | File comparison workflow | missing output fragment 'dim' |
| `bidsappaa` | FreeSurfer recon-all availability | missing output fragment 'recon-all' |
| `bidsappbaracus` | FreeSurfer atlas files | missing output fragment 'RB_all' |
| `bidsappmrtrix3connectome` | pipeline create brain mask | exit code 1, want 0 / mrthreshold: [00;31m[WARNING] existing output files will be overwritten[0m |
| `bidscoin` | bids-validator with JSON output | missing output fragment '{' |
| `brainager` | Full brainager pipeline with example data | exit code 1, want 0 / SPM12, version 7219 (standalone) |

### Tests In Bucket

| Suite | Test | Symptom |
| --- | --- | --- |
| `aidamri` | Nipype BET interface | exit code 1, want 0 / Traceback (most recent call last): |
| `aidamri` | Nipype FLIRT interface | exit code 1, want 0 / Traceback (most recent call last): |
| `aidamri` | Nipype MCFLIRT interface | exit code 1, want 0 / Traceback (most recent call last): |
| `ashs` | ASHS main help | missing output fragment 'ashs_main: automatic segmentation of hippocampal subfields' |
| `ashs` | ASHS train help | missing output fragment 'ashs_train: generate a new training set' |
| `ashs` | C3D affine tool help | missing output fragment 'RAS Affine Transform Tool' |
| `ashs` | FSL flirt help | missing output fragment 'FLIRT' |
| `ashs` | Greedy help | missing output fragment "greedy: Paul's greedy diffeomorphic registration" |
| `ashs` | NLMDenoise help | missing output fragment 'NLMDenoise: none local mean denoising' |
| `ashs` | NLMUpsample help | missing output fragment 'NLMUpsample: none local upsample' |
| `batchheudiconv` | File comparison workflow | missing output fragment 'dim' |
| `batchheudiconv` | nib-diff BOLD runs | exit code 255, want 1 |
| `batchheudiconv` | nib-diff different files | exit code 255, want 1 |
| `batchheudiconv` | nib-diff identical files | exit code 255, want 0 |
| `batchheudiconv` | nib-diff verbose | exit code 255, want 1 |
| `batchheudiconv` | nib-diff with data tolerance | exit code 255, want 0 |
| `batchheudiconv` | nib-diff with header fields | exit code 255, want 1 |
| `bidsappaa` | FreeSurfer recon-all availability | missing output fragment 'recon-all' |
| `bidsappbaracus` | FreeSurfer atlas files | missing output fragment 'RB_all' |
| `bidsappmrtrix3connectome` | pipeline create brain mask | exit code 1, want 0 / mrthreshold: [00;31m[WARNING] existing output files will be overwritten[0m |
| `bidsappmrtrix3connectome` | pipeline z-score normalization | exit code 1, want 0 / mrcalc: [00;31m[WARNING] existing output files will be overwritten[0m |
| `bidscoin` | bids-validator with JSON output | missing output fragment '{' |
| `bidscoin` | bidscoin test basic | missing output fragment 'Testing' |
| `bidscoin` | dcm2niix version check | missing output fragment 'dcm2niiX version' |
| `brainager` | Full brainager pipeline with example data | exit code 1, want 0 / SPM12, version 7219 (standalone) |
| `brainager` | Run brainager on test data T1w | exit code 1, want 0 / SPM12, version 7219 (standalone) |
| `brainnetviewer` | BrainNet binary has reasonable size | missing output fragment 'Binary size OK' |
| `cat12` | Prepare GM smoothing batch | exit code 1, want 0 / /bin/bash: line 1: echo: write error: Input/output error |
| `cat12` | Prepare IQR extraction batch | exit code 1, want 0 / /bin/bash: line 3: echo: write error: Input/output error |
| `cat12` | Prepare ROI extraction batch | exit code 1, want 0 / /bin/bash: line 3: echo: write error: Input/output error |
| `cat12` | Prepare TIV extraction batch | exit code 1, want 0 / /bin/bash: line 3: echo: write error: Input/output error |
| `cat12` | Prepare quality measures batch | exit code 1, want 0 / /bin/bash: line 3: echo: write error: Input/output error |
| `cat12` | Prepare smoothing batch | exit code 1, want 0 / /bin/bash: line 2: echo: write error: Input/output error |
| `cat12` | Verify CAT12 batch error handling | missing output fragment 'Bye for now' |
| `code` | Python help | missing output fragment 'usage: python' |
| `code` | VSCode Julia extension | exit code 1, want 0 / You are trying to start Visual Studio Code as a super user which isn't recommended. If this was intended, please add the argument `--no-s... |
| `code` | VSCode Jupyter extension | exit code 1, want 0 / You are trying to start Visual Studio Code as a super user which isn't recommended. If this was intended, please add the argument `--no-s... |
| `code` | VSCode Python extension | exit code 1, want 0 / You are trying to start Visual Studio Code as a super user which isn't recommended. If this was intended, please add the argument `--no-s... |
| `code` | VSCode list extensions | exit code 1, want 0 / You are trying to start Visual Studio Code as a super user which isn't recommended. If this was intended, please add the argument `--no-s... |
| `code` | VSCode version check | missing output fragment '1.76.1' |
| `dicompare` | Exported schema is readable | exit code 1, want 0 / Traceback (most recent call last): |
| `fastsurfer` | FreeSurferColorLUT exists | exit code 1, want 0 |
| `fsl` | fslhd - display header | missing output fragment 'dim1' |
| `fsl` | fslhd XML format | missing output fragment '<nifti_image' |
| `fsl` | pipeline - basic preprocessing | exit code 255, want 0 |
| `fsl` | pipeline - percent signal change | exit code 255, want 0 |
| `fsl` | pipeline - tSNR calculation | exit code 255, want 0 |
| `gigaconnectome` | connectivity analysis pipeline | exit code 255, want 0 |
| `gigaconnectome` | giga_connectome invalid bids dir | missing output fragment 'error' |
| `glmsingle` | GLM_single HRF model parameter | missing output fragment 'optimise' |
| `glmsingle` | GLM_single default parameters | missing output fragment 'n_pcs 10' |
| `glmsingle` | GLM_single n_boots parameter | missing output fragment 'n_boots 100' |
| `glmsingle` | GLM_single wantfracridge parameter | missing output fragment 'wantfracridge 1' |
| `glmsingle` | GLM_single wantglmdenoise parameter | missing output fragment 'wantglmdenoise 1' |
| `glmsingle` | GLM_single wantlibrary parameter | missing output fragment 'wantlibrary 1' |
| `glmsingle` | GLMsingle minimal workflow test | missing output fragment 'GLMsingle configured successfully' |
| `laynii` | LN2_MULTILATERATE columns | exit code 255, want 139 / ======================= |
| `lcmodel` | KECC responds to input | missing output fragment 'runtime error' |
| `lcmodel` | LCModel responds to input | exit code 1, want 0 |
| `lcmodel` | Makebasis responds to input | exit code 1, want 0 |
| `lcmodel` | Plotraw responds to input | exit code 1, want 0 |
| `lesymap` | R BATCH mode | missing output fragment 'BATCH mode test successful' |
| `lesymap` | Rscript file execution | missing output fragment 'Script executed successfully' |
| `linda` | R BATCH mode | missing output fragment 'BATCH mode test successful' |
| `linda` | Rscript file execution | missing output fragment 'Script executed successfully' |
| `micapipe` | ANTs CreateImage blank | exit code 255, want 134 / terminate called after throwing an instance of 'std::logic_error' |
| `micapipe` | Format conversion roundtrip | exit code 255, want 0 / /opt/freesurfer-7.3.2/bin/mri_convert test_output/roundtrip.nii.gz test_output/roundtrip.mgz |
| `micapipe` | FreeSurfer mri_binarize | exit code 255, want 0 / -------------------------------------------------------------------------- |
| `micapipe` | FreeSurfer mri_convert conform | exit code 255, want 0 / /opt/freesurfer-7.3.2/bin/mri_convert -c ds000001/sub-01/anat/sub-01_T1w.nii.gz test_output/t1w_conformed.mgz |
| `micapipe` | FreeSurfer mri_convert format conversion | exit code 255, want 0 / /opt/freesurfer-7.3.2/bin/mri_convert ds000001/sub-01/anat/sub-01_T1w.nii.gz test_output/t1w_fs.mgz |
| `micapipe` | FreeSurfer mri_convert reorient | exit code 255, want 0 / /opt/freesurfer-7.3.2/bin/mri_convert --out_orientation RAS ds000001/sub-01/anat/sub-01_T1w.nii.gz test_output/t1w_ras.nii.gz |
| `micapipe` | FreeSurfer mri_convert resample | exit code 255, want 0 / /opt/freesurfer-7.3.2/bin/mri_convert -vs 1 1 1 ds000001/sub-01/anat/sub-01_T1w.nii.gz test_output/t1w_1mm.nii.gz |
| `minc` | ANTs version check | missing output fragment 'ANTs' |
| `minc` | Convert BOLD NIfTI to MINC | exit code 1, want 0 / Traceback (most recent call last): |
| `minc` | Convert T1w NIfTI to MINC | exit code 1, want 0 / Traceback (most recent call last): |
| `minc` | Convert T2 NIfTI to MINC | exit code 1, want 0 / Traceback (most recent call last): |
| `minc` | Version check (mincinfo) | missing output fragment 'program' |
| `mricron` | Resources directory in PATH | missing output fragment 'mricron path present' |
| `mriqc` | ANTs N4BiasFieldCorrection on T1w | exit code 255, want 0 |
| `mriqc` | Calculate DVARS for BOLD | exit code 255, want 0 |
| `mriqc` | Nilearn std BOLD | exit code 255, want 0 |
| `mriqc` | Nilearn tSNR calculation | exit code 255, want 0 |
| `mrsiproc` | FAST tissue segmentation | exit code 255, want 0 |
| `nftsim` | Verbose output mode | missing output fragment 'Time:' |
| `nftsim` | Verify alpha parameter | missing output fragment 'alpha:' |
| `nftsim` | Verify beta parameter | missing output fragment 'beta:' |
| `nftsim` | Verify coupling parameters in output | missing output fragment 'Coupling' |
| `nftsim` | Verify dendrite parameters | missing output fragment 'Dendrite' |
| `nftsim` | Verify firing parameters | missing output fragment 'Qmax' |
| `nftsim` | Verify map propagator | missing output fragment 'Map' |
| `nftsim` | Verify node count in E cortical | missing output fragment '900' |
| `nftsim` | Verify node count in EI cortical | missing output fragment '2048' |
| `nftsim` | Verify node count in EIRS | missing output fragment '144' |
| `nftsim` | Verify output column headers | missing output fragment 'Time' |
| `nftsim` | Verify population parameters in output | missing output fragment 'Population' |
| `nftsim` | Verify propagator parameters in output | missing output fragment 'Propagator' |
| `nftsim` | Verify pulse stimulus in stimuli-only | missing output fragment 'Pulse' |
| `nftsim` | Verify sigmoid firing function | missing output fragment 'Sigmoid' |
| `nftsim` | Verify simulation duration in EIRS | missing output fragment '15' |
| `nftsim` | Verify time step in EIRS | missing output fragment 'Deltat:' |
| `nftsim` | Verify wave propagator | missing output fragment 'Wave' |
| `nftsim` | Verify white noise stimulus | missing output fragment 'White' |
| `nibabies` | BIDS validator help | missing output fragment 'bids-validator' |
| `nibabies` | FSL header dump | missing output fragment 'sizeof_hdr' |
| `nibabies` | FreeSurfer mri_convert help | missing output fragment 'mri_convert' |
| `nibabies` | FreeSurfer mri_info | missing output fragment 'dimensions' |
| `nibabies` | NiBabies help output | missing output fragment 'NiBabies' |
| `osprey` | T1w structural image readable (sub-01) | exit code 1, want 0 |
| `osprey` | T1w structural image readable (sub-02) | exit code 1, want 0 |
| `qmrlab` | AMICO model check | exit code 1, want 0 |
| `qsiprep` | ANTs CreateImage | exit code 255, want 134 / terminate called after throwing an instance of 'std::logic_error' |
| `qsiprep` | MRtrix3 pipeline - mask and apply | exit code 1, want 0 / mrcalc: [00;31m[WARNING] existing output files will be overwritten[0m |
| `qsmxt` | FASTSURFER_HOME set | missing output fragment 'FastSurfer' |
| `qsmxt` | JULIA_DEPOT_PATH set | missing output fragment 'julia_depot' |
| `romeo` | Multi-echo B0 calculation | exit code 1, want 0 / ERROR: cannot mmap a gzipped NIfTI file |
| `romeo` | Multi-echo individual unwrapping | exit code 1, want 0 / ERROR: cannot mmap a gzipped NIfTI file |
| `romeo` | Multi-echo phase offset correction | exit code 1, want 0 / ERROR: cannot mmap a gzipped NIfTI file |
| `romeo` | Multi-echo temporal unwrapping | exit code 1, want 0 / ERROR: cannot mmap a gzipped NIfTI file |
| `romeo` | Unwrap synthetic phase basic | exit code 1, want 0 / ERROR: cannot mmap a gzipped NIfTI file |
| `romeo` | Unwrap synthetic phase with custom mask | exit code 1, want 0 / ERROR: cannot mmap a gzipped NIfTI file |
| `romeo` | Unwrap synthetic phase with magnitude | exit code 1, want 0 / ERROR: cannot mmap a gzipped NIfTI file |
| `root` | RDataFrame from TTree | missing output fragment 'Tree entries 1000' |
| `root` | Read histogram from file | missing output fragment 'Entries 10000' |
| `root` | Read tree from file | missing output fragment 'Tree entries 1000' |
| `root` | TChain multiple files | missing output fragment 'Chain entries 20' |
| `root` | TTree Draw histogram | missing output fragment 'Histogram entries 1000' |
| `root` | TTree Project histogram | missing output fragment 'Projected 1000' |
| `root` | rootls basic listing | exit code 1, want 0 / Error in <TFile::ReadBuffer>: error reading all requested bytes from file test_output/test_hist.root, got 63 of 300 |
| `root` | rootls long listing | exit code 1, want 0 / Error in <TFile::ReadBuffer>: error reading all requested bytes from file test_output/test_hist.root, got 63 of 300 |
| `root` | rootls tree listing | exit code 1, want 0 / Error in <TFile::ReadBuffer>: error reading all requested bytes from file test_output/test_tree.root, got 63 of 300 |
| `root` | rootmkdir create directory | exit code 1, want 0 / Error in <TFile::ReadBuffer>: error reading all requested bytes from file test_output/test_hist.root, got 63 of 300 |
| `sigviewer` | Full help with Qt options | missing output fragment '--platform' |
| `sigviewer` | Help information | missing output fragment 'SigViewer - a biosignal viewer' |
| `sigviewer` | Help shows file argument | missing output fragment 'Input file (optional)' |
| `sigviewer` | Invalid option handling | missing output fragment 'Unknown option' |
| `sigviewer` | Multiple help invocations | missing output fragment 'SigViewer 0.6.4' |
| `sigviewer` | Version check | missing output fragment 'SigViewer 0.6.4' |
| `sovabids` | Check sovaconvert CLI exists | exit code 1, want 0 |
| `sovabids` | Check sovapply CLI exists | exit code 1, want 0 |
| `spm12` | Create imcalc batch file | missing output fragment 'imcalc' |
| `spm12` | Create imcalc threshold batch | missing output fragment 'imcalc' |
| `spm12` | Create smoothing batch file | missing output fragment 'matlabbatch' |

## Other / needs targeted repro

Tests: 1 across 1 suites.

Evidence: Single leftover that does not fit the high-confidence buckets.

Likely code surface: Re-run after larger buckets are fixed; likely becomes obvious once cascades are removed.

### Affected Suites

| Suite | Tests |
| --- | ---: |
| `ezbids` | 1 |

### Representative Failures

| Suite | Test | Tinyrange symptom |
| --- | --- | --- |
| `ezbids` | npm version | exit code 251, want 0 / npm ERR! code EIO |

### Tests In Bucket

| Suite | Test | Symptom |
| --- | --- | --- |
| `ezbids` | npm version | exit code 251, want 0 / npm ERR! code EIO |
