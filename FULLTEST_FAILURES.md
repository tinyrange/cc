# PyNeurodesk Fulltest Failing Tests

Source: completed-suite artifacts from https://github.com/tinyrange/cc/actions/runs/24991386248.

Total parsed failing tests: 871.

## aidamri

- Passed: 82
- Failed: 3
- Skipped: 0

- `Nipype BET interface` [nonzero_exit]: exit code 1, want 0
- `Nipype FLIRT interface` [command_not_found]: exit code 1, want 0
- `Nipype MCFLIRT interface` [command_not_found]: exit code 1, want 0

## ants

- Passed: 97
- Failed: 5
- Skipped: 0

- `N4 with custom convergence` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ants/.pyneurodesk-fulltest-activate.sh\nN4BiasFieldCorrection -d 3 -i ds000001/sub-01/anat/sub-01_T1w.nii.gz -o test_output/t1w_n4_custom.n...
- `Denoise image Rician model` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ants/.pyneurodesk-fulltest-activate.sh\nDenoiseImage -d 3 -i ds000001/sub-01/anat/sub-01_T1w.nii.gz -n Rician -o test_output/t1w_denoise_ri...
- `Denoise image Gaussian model` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ants/.pyneurodesk-fulltest-activate.sh\nDenoiseImage -d 3 -i ds000001/sub-01/anat/sub-01_T1w.nii.gz -n Gaussian -o test_output/t1w_denoise_...
- `Denoise with noise image output` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ants/.pyneurodesk-fulltest-activate.sh\nDenoiseImage -d 3 -i ds000001/sub-01/anat/sub-01_T1w.nii.gz -n Gaussian -o [test_output/t1w_denoise...
- `Affine initializer` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ants/.pyneurodesk-fulltest-activate.sh\nantsAffineInitializer 3 ds000001/sub-01/anat/sub-01_T1w.nii.gz ds000001/sub-01/anat/sub-01_inplaneT...

## ashs

- Passed: 24
- Failed: 55
- Skipped: 15

- `ASHS main help` [missing_output]: missing output fragment 'ashs_main: automatic segmentation of hippocampal subfields'
- `ASHS train help` [missing_output]: missing output fragment 'ashs_train: generate a new training set'
- `ASHS root environment` [missing_output]: missing output fragment '/opt/ashs'
- `C3D version` [command_not_found]: exit code 127, want 0
- `C3D image info` [command_not_found]: exit code 127, want 0
- `C3D image info full` [command_not_found]: exit code 127, want 0
- `C3D resample` [command_not_found]: exit code 127, want 0
- `C3D smooth` [command_not_found]: exit code 127, want 0
- `C3D threshold` [command_not_found]: exit code 127, want 0
- `C3D multiply` [command_not_found]: exit code 127, want 0
- `C3D add constant` [command_not_found]: exit code 127, want 0
- `C3D scale` [command_not_found]: exit code 127, want 0
- `C3D clip` [command_not_found]: exit code 127, want 0
- `C3D stretch` [command_not_found]: exit code 127, want 0
- `C3D sqrt` [command_not_found]: exit code 127, want 0
- `C3D log` [command_not_found]: exit code 127, want 0
- `C3D exp` [command_not_found]: exit code 127, want 0
- `C3D flip` [command_not_found]: exit code 127, want 0
- `C3D orient` [command_not_found]: exit code 127, want 0
- `C3D pad` [command_not_found]: exit code 127, want 0
- `C3D reslice identity` [command_not_found]: exit code 127, want 0
- `C3D voxel sum` [command_not_found]: exit code 127, want 0
- `C3D affine tool help` [missing_output]: missing output fragment 'RAS Affine Transform Tool'
- `C3D affine create rotation` [command_not_found]: exit code 127, want 0
- `C3D affine create scaling` [command_not_found]: exit code 127, want 0
- `C3D affine inverse` [command_not_found]: exit code 127, want 0
- `C3D affine info` [command_not_found]: exit code 127, want 0
- `C3D affine export ITK` [command_not_found]: exit code 127, want 0
- `C3D affine from sform` [command_not_found]: exit code 127, want 0
- `Greedy help` [missing_output]: missing output fragment "greedy: Paul's greedy diffeomorphic registration"
- `Greedy version` [command_not_found]: exit code 127, want 0
- `Greedy affine registration` [command_not_found]: exit code 127, want 0
- `Greedy rigid registration` [command_not_found]: exit code 127, want 0
- `Greedy moments initialization` [command_not_found]: exit code 127, want 0
- `Greedy deformable registration` [command_not_found]: exit code 127, want 0
- `Label fusion version` [command_not_found]: exit code 127, want 0
- `NLMDenoise help` [missing_output]: missing output fragment 'NLMDenoise: none local mean denoising'
- `NLMDenoise run` [command_not_found]: exit code 127, want 0
- `NLMDenoise Rician` [command_not_found]: exit code 127, want 0
- `NLMUpsample help` [missing_output]: missing output fragment 'NLMUpsample: none local upsample'
- `NLMUpsample run` [command_not_found]: exit code 127, want 0
- `ANTs N3BiasFieldCorrection run` [command_not_found]: exit code 127, want 0
- `ANTs ThresholdImage run` [command_not_found]: exit code 127, want 0
- `ANTs ImageMath normalize` [command_not_found]: exit code 127, want 0
- `ANTs ImageMath gradient` [command_not_found]: exit code 127, want 0
- `ANTs AverageImages run` [command_not_found]: exit code 127, want 0
- `ANTs MultiplyImages run` [command_not_found]: exit code 127, want 0
- `ANTs MeasureMinMaxMean run` [command_not_found]: exit code 127, want 0
- `FSL bet2 run` [command_not_found]: exit code 127, want 0
- `FSL flirt help` [missing_output]: missing output fragment 'FLIRT'
- `FSL flirt run` [command_not_found]: exit code 127, want 0
- `Pipeline preprocessing` [command_not_found]: exit code 127, want 0
- `Pipeline brain extraction and masking` [command_not_found]: exit code 127, want 0
- `Pipeline registration chain` [command_not_found]: exit code 127, want 0
- `Pipeline morphology operations` [command_not_found]: exit code 127, want 0

## aslprep

- Passed: 93
- Failed: 1
- Skipped: 0

- `fslcpgeom - copy geometry` [command_not_found]: exit code 127, want 0

## batchheudiconv

- Passed: 92
- Failed: 10
- Skipped: 5

- `Create study directory structure` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-batchheudiconv/.pyneurodesk-fulltest-activate.sh\ncd test_output && bh01_prep_dir.sh test_study 2>&1']' timed out after 120.0 seconds
- `nib-ls BOLD with stats` [nonzero_exit]: exit code 1, want 0
- `nib-diff identical files` [nonzero_exit]: exit code 255, want 0
- `nib-diff different files` [nonzero_exit]: exit code 255, want 1
- `nib-diff BOLD runs` [nonzero_exit]: exit code 255, want 1
- `nib-diff with header fields` [nonzero_exit]: exit code 255, want 1
- `nib-diff verbose` [nonzero_exit]: exit code 255, want 1
- `nib-diff with data tolerance` [nonzero_exit]: exit code 255, want 0
- `Multi-file NIfTI inspection` [nonzero_exit]: exit code 1, want 0
- `File comparison workflow` [missing_output]: missing output fragment 'dim'

## bidsapphcppipelines

- Passed: 83
- Failed: 3
- Skipped: 0

- `FREESURFER_HOME set correctly` [missing_output]: missing output fragment '/opt/freesurfer'
- `HCPPIPEDIR set correctly` [missing_output]: missing output fragment '/opt/HCP-Pipelines'
- `CARET7DIR set correctly` [missing_output]: missing output fragment '/opt/workbench'

## bidsappmrtrix3connectome

- Passed: 114
- Failed: 6
- Skipped: 4

- `mredit voxel value` [command_not_found]: exit code 127, want 0
- `transformcalc invert` [command_not_found]: exit code 127, want 0
- `label2colour convert` [command_not_found]: exit code 127, want 0
- `pipeline create brain mask` [nonzero_exit]: exit code 1, want 0
- `pipeline z-score normalization` [nonzero_exit]: exit code 1, want 0
- `peaks2amp convert` [command_not_found]: exit code 127, want 0

## bidsappspm

- Passed: 36
- Failed: 7
- Skipped: 12

- `SPM_DIR environment variable` [missing_output]: missing output fragment '/opt/spm12'
- `SPM_EXEC environment variable` [missing_output]: missing output fragment '/opt/spm12/spm12'
- `MCR_VERSION environment variable` [missing_output]: missing output fragment 'v97'
- `PATH includes SPM directory` [missing_output]: missing output fragment '/opt/spm12'
- `Participant preprocessing - single subject` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-bidsappspm/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-1effe745ecfb38b6 -- bash -lc '/opt/spm12/run.sh ds000001 test_o...
- `Multi-subject preprocessing` [nonzero_exit]: exit code 1, want 0
- `LD_LIBRARY_PATH includes MCR` [missing_output]: missing output fragment '/opt/mcr/v97'

## bidscoin

- Passed: 92
- Failed: 4
- Skipped: 0

- `BIDScoin version check` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-bidscoin/.pyneurodesk-fulltest-activate.sh\nbidscoin --version 2>&1']' timed out after 120.0 seconds
- `dcm2niix version check` [missing_output]: missing output fragment 'dcm2niiX version'
- `bids-validator with JSON output` [missing_output]: missing output fragment '{'
- `bidscoin test basic` [missing_output]: missing output fragment 'Testing'

## brainager

- Passed: 36
- Failed: 4
- Skipped: 8

- `Full brainager pipeline with example data` [nonzero_exit]: exit code 1, want 0
- `Run brainager on test data T1w` [nonzero_exit]: exit code 1, want 0
- `Check LD_LIBRARY_PATH` [missing_output]: missing output fragment '/opt/mcr'
- `Check PATH includes brainageR` [missing_output]: missing output fragment '/opt/brainageR'

## brainnetviewer

- Passed: 87
- Failed: 2
- Skipped: 0

- `DEPLOY_BINS environment variable` [missing_output]: missing output fragment 'brainnetviewer'
- `BrainNet binary has reasonable size` [missing_output]: missing output fragment 'Binary size OK'

## brainstorm

- Passed: 49
- Failed: 1
- Skipped: 0

- `Brainstorm defaults directory structure` [missing_output]: missing output fragment 'anatomy'

## connectomeworkbench

- Passed: 107
- Failed: 5
- Skipped: 0

- `Volume extrema - presmooth` [nonzero_exit]: exit code 255, want 134
- `Volume find clusters` [nonzero_exit]: exit code 255, want 139
- `Volume find clusters - less than` [nonzero_exit]: exit code 255, want 139
- `Volume find clusters - size ratio` [nonzero_exit]: exit code 255, want 139
- `Pipeline - cluster analysis` [nonzero_exit]: exit code 255, want 139

## dcm2bids

- Passed: 39
- Failed: 4
- Skipped: 17

- `dcm2niix BIDS sidecar only mode` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-dcm2bids/.pyneurodesk-fulltest-activate.sh\nmkdir -p test_output/dcm2niix_test/sidecar_only && \\\ncp ds000001/sub-01/anat/sub-01_T1w.nii.g...
- `Create mock NIfTI with JSON sidecar (T1w)` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-dcm2bids/.pyneurodesk-fulltest-activate.sh\nmkdir -p test_output/mock_dicom && \\\ncp ds000001/sub-01/anat/sub-01_T1w.nii.gz test_output/mo...
- `Create mock NIfTI with JSON sidecar (BOLD)` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-dcm2bids/.pyneurodesk-fulltest-activate.sh\ncp ds000001/sub-01/func/sub-01_task-balloonanalogrisktask_run-01_bold.nii.gz test_output/mock_d...
- `Create mock T2w data` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-dcm2bids/.pyneurodesk-fulltest-activate.sh\ncp ds000001/sub-01/anat/sub-01_inplaneT2.nii.gz test_output/mock_dicom/005_T2_20230101120000.ni...

## deepretinotopy

- Passed: 86
- Failed: 48
- Skipped: 1

- `FreeSurfer version` [nonzero_exit]: exit code 1, want 0
- `FreeSurfer help` [missing_output]: missing output fragment 'FreeSurfer'
- `mri_info available` [missing_output]: missing output fragment 'mri_info'
- `mri_convert available` [missing_output]: missing output fragment 'mri_convert'
- `mri_binarize available` [missing_output]: missing output fragment 'mri_binarize'
- `mri_vol2vol available` [missing_output]: missing output fragment 'mri_vol2vol'
- `mri_vol2surf available` [missing_output]: missing output fragment 'mri_vol2surf'
- `mri_surf2surf available` [missing_output]: missing output fragment 'mri_surf2surf'
- `mri_robust_register available` [missing_output]: missing output fragment 'mri_robust_register'
- `mri_synthstrip available` [missing_output]: missing output fragment 'SynthStrip'
- `mri_synthseg available` [missing_output]: missing output fragment 'SynthSeg'
- `mris_convert available` [missing_output]: missing output fragment 'mris_convert'
- `mris_curvature available` [missing_output]: missing output fragment 'mris_curvature'
- `mris_info available` [missing_output]: missing output fragment 'mris_info'
- `mris_register available` [missing_output]: missing output fragment 'mris_register'
- `mris_inflate available` [missing_output]: missing output fragment 'mris_inflate'
- `mris_sphere available` [missing_output]: missing output fragment 'mris_sphere'
- `mris_smooth available` [missing_output]: missing output fragment 'mris_smooth'
- `mris_anatomical_stats available` [missing_output]: missing output fragment 'mris_anatomical_stats'
- `recon-all available` [missing_output]: missing output fragment 'recon-all'
- `recon-all stages` [missing_output]: missing output fragment 'all'
- `mri_info on T1w` [missing_output]: missing output fragment 'voxel sizes'
- `mri_info dimensions T1w` [nonzero_exit]: exit code 1, want 0
- `mri_info voxel size T1w` [nonzero_exit]: exit code 1, want 0
- `mri_info on T2` [missing_output]: missing output fragment 'voxel sizes'
- `mri_info on BOLD` [missing_output]: missing output fragment 'voxel sizes'
- `mri_convert format test` [nonzero_exit]: exit code 1, want 0
- `mri_binarize threshold` [nonzero_exit]: exit code 1, want 0
- `mri_convert resample` [nonzero_exit]: exit code 1, want 0
- `mri_convert orientation` [nonzero_exit]: exit code 1, want 0
- `mri_annotation2label available` [missing_output]: missing output fragment 'mri_annotation2label'
- `mri_label2vol available` [missing_output]: missing output fragment 'mri_label2vol'
- `mris_label2annot available` [missing_output]: missing output fragment 'mris_label2annot'
- `mri_watershed available` [missing_output]: missing output fragment 'mri_watershed'
- `mri_segment available` [missing_output]: missing output fragment 'mri_segment'
- `mri_ca_label available` [missing_output]: missing output fragment 'mri_ca_label'
- `mri_aparc2aseg available` [missing_output]: missing output fragment 'mri_aparc2aseg'
- `mri_segstats available` [missing_output]: missing output fragment 'mri_segstats'
- `mri_coreg available` [missing_output]: missing output fragment 'mri_coreg'
- `bbregister available` [missing_output]: missing output fragment 'bbregister'
- `mri_em_register available` [missing_output]: missing output fragment 'mri_em_register'
- `mri_ca_register available` [missing_output]: missing output fragment 'mri_ca_register'
- `mri_glmfit available` [missing_output]: missing output fragment 'mri_glmfit'
- `mri_surfcluster available` [missing_output]: missing output fragment 'mri_surfcluster'
- `mri_volcluster available` [missing_output]: missing output fragment 'mri_volcluster'
- `mri_fwhm available` [missing_output]: missing output fragment 'mri_fwhm'
- `mri_concatenate_lta available` [missing_output]: missing output fragment 'mri_concatenate_lta'
- `mri_warp_convert available` [missing_output]: missing output fragment 'mri_warp_convert'

## dicompare

- Passed: 101
- Failed: 9
- Skipped: 0

- `start.sh contains serve command` [missing_output]: missing output fragment 'serve -s dist -l 3001'
- `Web server can start` [nonzero_exit]: exit code 255, want 0
- `Web server serves index.html` [nonzero_exit]: exit code 255, want 0
- `Web server serves favicon` [nonzero_exit]: exit code 255, want 0
- `Web server serves manifest.json` [nonzero_exit]: exit code 255, want 0
- `Web server serves schema files` [nonzero_exit]: exit code 255, want 0
- `Web server serves validation functions` [nonzero_exit]: exit code 255, want 0
- `Web server single page mode works` [nonzero_exit]: exit code 255, want 0
- `Exported schema is readable` [nonzero_exit]: exit code 1, want 0

## diffusiontoolkit

- Passed: 168
- Failed: 2
- Skipped: 0

- `dtk binary exists` [nonzero_exit]: exit code 1, want 0
- `Diffusion toolkit in PATH` [missing_output]: missing output fragment '/opt/diffusiontoolkit'

## dsistudio

- Passed: 80
- Failed: 3
- Skipped: 0

- `atk action - track corticospinal tract` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-dsistudio/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-2c9de1a7489b2ec8 -- bash -lc 'env QT_QPA_PLATFORM=offscreen dsi_...
- `atk action - track multiple bundles` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-dsistudio/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-2c9de1a7489b2ec8 -- bash -lc 'env QT_QPA_PLATFORM=offscreen dsi_...
- `atk action - optic radiation` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-dsistudio/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-2c9de1a7489b2ec8 -- bash -lc 'env QT_QPA_PLATFORM=offscreen dsi_...

## eeglab

- Passed: 67
- Failed: 6
- Skipped: 0

- `PATH includes EEGLAB directory` [missing_output]: missing output fragment '/opt/eeglab-2020.0/'
- `LD_LIBRARY_PATH includes MCR runtime` [missing_output]: missing output fragment '/opt/MCR/v98/runtime/glnxa64'
- `LD_LIBRARY_PATH includes MCR bin` [missing_output]: missing output fragment '/opt/MCR/v98/bin/glnxa64'
- `LD_LIBRARY_PATH includes MCR sys/os` [missing_output]: missing output fragment '/opt/MCR/v98/sys/os/glnxa64'
- `XAPPLRESDIR set correctly` [missing_output]: missing output fragment '/opt/MCR/v98'
- `DEPLOY_BINS indicates EEGLAB` [missing_output]: missing output fragment 'EEGLAB'

## ezbids

- Passed: 85
- Failed: 4
- Skipped: 0

- `npm version` [nonzero_exit]: exit code 251, want 0
- `pm2 version` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ezbids/.pyneurodesk-fulltest-activate.sh\npm2 --version']' timed out after 120.0 seconds
- `Complete T1w processing pipeline` [nonzero_exit]: exit code 137, want 0
- `Brain extraction and defacing pipeline` [nonzero_exit]: exit code 137, want 0

## fastsurfer

- Passed: 67
- Failed: 3
- Skipped: 0

- `FS_LICENSE environment variable` [missing_output]: missing output fragment '/opt/license.txt'
- `FREESURFER_HOME set` [missing_output]: missing output fragment '/opt/freesurfer'
- `FreeSurferColorLUT exists` [nonzero_exit]: exit code 1, want 0

## fmriprep

- Passed: 85
- Failed: 2
- Skipped: 1

- `DenoiseImage on T1w` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-fmriprep/.pyneurodesk-fulltest-activate.sh\nDenoiseImage -d 3 -i ds000001/sub-01/anat/sub-01_T1w.nii.gz -o test_output/ants/t1w_denoised.ni...
- `FSL fslsplit temporal` [command_not_found]: exit code 127, want 0

## freesurfer

- Passed: 19
- Failed: 52
- Skipped: 10

- `FreeSurfer version check` [nonzero_exit]: exit code 1, want 0
- `recon-all version check` [missing_output]: missing output fragment 'freesurfer'
- `mri_info basic` [nonzero_exit]: exit code 1, want 0
- `mri_info dimensions` [nonzero_exit]: exit code 1, want 0
- `mri_info resolution` [nonzero_exit]: exit code 1, want 0
- `mri_info orientation` [nonzero_exit]: exit code 1, want 0
- `mri_info vox2ras` [nonzero_exit]: exit code 1, want 0
- `mri_info TR` [nonzero_exit]: exit code 1, want 0
- `mri_info nframes` [nonzero_exit]: exit code 1, want 0
- `mri_convert NIfTI to MGZ` [nonzero_exit]: exit code 1, want 0
- `mri_convert conform` [nonzero_exit]: exit code 1, want 0
- `mri_convert resample` [nonzero_exit]: exit code 1, want 0
- `mri_convert crop` [nonzero_exit]: exit code 1, want 0
- `mri_convert reorient` [nonzero_exit]: exit code 1, want 0
- `mri_convert frame extraction` [nonzero_exit]: exit code 1, want 0
- `mri_convert data type change` [nonzero_exit]: exit code 1, want 0
- `mri_binarize threshold` [nonzero_exit]: exit code 1, want 0
- `mri_binarize range` [nonzero_exit]: exit code 1, want 0
- `mri_binarize invert` [nonzero_exit]: exit code 1, want 0
- `mri_binarize dilate` [nonzero_exit]: exit code 1, want 0
- `mri_binarize erode` [nonzero_exit]: exit code 1, want 0
- `mri_binarize percentage` [nonzero_exit]: exit code 1, want 0
- `mri_concat mean` [nonzero_exit]: exit code 1, want 0
- `mri_concat std` [nonzero_exit]: exit code 1, want 0
- `mri_concat max` [nonzero_exit]: exit code 1, want 0
- `mri_concat sum` [nonzero_exit]: exit code 1, want 0
- `mri_concat multiply` [nonzero_exit]: exit code 1, want 0
- `mri_vol2vol regheader` [nonzero_exit]: exit code 1, want 0
- `mri_vol2vol nearest neighbor` [nonzero_exit]: exit code 1, want 0
- `mri_vol2vol cubic` [nonzero_exit]: exit code 1, want 0
- `mri_vol2vol downsample` [nonzero_exit]: exit code 1, want 0
- `mri_coreg basic` [nonzero_exit]: exit code 1, want 0
- `mri_coreg 9dof` [nonzero_exit]: exit code 1, want 0
- `mri_coreg 12dof` [nonzero_exit]: exit code 1, want 0
- `mri_robust_register basic` [nonzero_exit]: exit code 1, want 0
- `mri_robust_register affine` [nonzero_exit]: exit code 1, want 0
- `mri_robust_register with output` [nonzero_exit]: exit code 1, want 0
- `mri_synthstrip basic` [nonzero_exit]: exit code 1, want 0
- `mri_synthstrip no csf` [nonzero_exit]: exit code 1, want 0
- `mri_synthstrip border` [nonzero_exit]: exit code 1, want 0
- `mri_nu_correct basic` [nonzero_exit]: exit code 1, want 0
- `AntsN4BiasFieldCorrectionFs basic` [nonzero_exit]: exit code 1, want 0
- `mri_diff identical` [missing_output]: missing output fragment 'diffcount 0'
- `mris_convert help` [missing_output]: missing output fragment 'mris_convert'
- `samseg help` [missing_output]: missing output fragment 'samseg'
- `bbregister help` [missing_output]: missing output fragment 'bbregister'
- `mri_em_register help` [missing_output]: missing output fragment 'mri_em_register'
- `mri_ca_label help` [missing_output]: missing output fragment 'mri_ca_label'
- `mri_aparc2aseg help` [missing_output]: missing output fragment 'mri_aparc2aseg'
- `mri_synthmorph help` [missing_output]: missing output fragment 'synthmorph'
- `mri_synthsr help` [missing_output]: missing output fragment 'synthsr'
- `mri_WMHsynthseg help` [missing_output]: missing output fragment 'WMH'

## fsl

- Passed: 116
- Failed: 11
- Skipped: 0

- `fslhd - display header` [missing_output]: missing output fragment 'dim1'
- `fslhd XML format` [missing_output]: missing output fragment '<nifti_image'
- `fslcpgeom - copy geometry` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-fsl/.pyneurodesk-fulltest-activate.sh\ncp test_output/t1w_abs.nii.gz test_output/t1w_cpgeom.nii.gz && fslcpgeom ds000001/sub-01/anat/sub-01...
- `fsl_motion_outliers - DVARS` [nonzero_exit]: exit code 137, want 0
- `fsl_motion_outliers - FD` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-fsl/.pyneurodesk-fulltest-activate.sh\nfsl_motion_outliers -i ds000001/sub-01/func/sub-01_task-balloonanalogrisktask_run-01_bold.nii.gz -o ...
- `fsl_glm - basic regression` [command_not_found]: exit code 127, want 0
- `fsl_regfilt - remove confounds` [command_not_found]: exit code 127, want 0
- `fsl-cluster - find clusters` [command_not_found]: exit code 127, want 0
- `pipeline - tSNR calculation` [nonzero_exit]: exit code 255, want 0
- `pipeline - percent signal change` [nonzero_exit]: exit code 255, want 0
- `pipeline - basic preprocessing` [nonzero_exit]: exit code 255, want 0

## gigaconnectome

- Passed: 90
- Failed: 3
- Skipped: 0

- `pybids layout creation` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-gigaconnectome/.pyneurodesk-fulltest-activate.sh\nmkdir -p test_output/bids_layout && pybids layout ds000001 test_output/bids_layout --no-v...
- `giga_connectome invalid bids dir` [missing_output]: missing output fragment 'error'
- `connectivity analysis pipeline` [nonzero_exit]: exit code 255, want 0

## globus

- Passed: 43
- Failed: 28
- Skipped: 0

- `Help display` [missing_output]: missing output fragment 'Usage'
- `Help display with -h flag` [missing_output]: missing output fragment 'Usage'
- `Help shows GUI mode` [nonzero_exit]: exit code 1, want 0
- `Help shows setup mode` [nonzero_exit]: exit code 1, want 0
- `Help shows start mode` [nonzero_exit]: exit code 1, want 0
- `Help shows stop mode` [nonzero_exit]: exit code 1, want 0
- `Help shows status mode` [nonzero_exit]: exit code 1, want 0
- `Help shows debug mode` [nonzero_exit]: exit code 1, want 0
- `Help shows trace mode` [nonzero_exit]: exit code 1, want 0
- `Help shows pause options` [nonzero_exit]: exit code 1, want 0
- `Help shows dir option` [nonzero_exit]: exit code 1, want 0
- `Help shows restrict-paths option` [nonzero_exit]: exit code 1, want 0
- `Help shows shared-paths option` [nonzero_exit]: exit code 1, want 0
- `Help shows GUI force option` [nonzero_exit]: exit code 1, want 0
- `Setup help display` [nonzero_exit]: exit code 1, want 0
- `Setup help shows name option` [nonzero_exit]: exit code 1, want 0
- `Setup help shows description option` [nonzero_exit]: exit code 1, want 0
- `Setup help shows owner option` [nonzero_exit]: exit code 1, want 0
- `Setup help shows setup-key option` [nonzero_exit]: exit code 1, want 0
- `Setup help shows high-assurance option` [nonzero_exit]: exit code 1, want 0
- `Setup help shows environment option` [nonzero_exit]: exit code 1, want 0
- `Setup help shows auto-description option` [nonzero_exit]: exit code 1, want 0
- `Setup help shows attributes option` [nonzero_exit]: exit code 1, want 0
- `Setup help shows authentication timeout option` [nonzero_exit]: exit code 1, want 0
- `Help shows path restriction examples` [nonzero_exit]: exit code 1, want 0
- `Help shows read-only path example` [nonzero_exit]: exit code 1, want 0
- `Help shows wildcard path example` [nonzero_exit]: exit code 1, want 0
- `Help shows character class example` [nonzero_exit]: exit code 1, want 0

## halfpipe

- Passed: 56
- Failed: 1
- Skipped: 0

- `fMRIPrep help` [missing_output]: missing output fragment 'fMRIPrep'

## hdbet

- Passed: 8
- Failed: 15
- Skipped: 0

- `Brain extraction fast mode CPU` [nonzero_exit]: exit code 1, want 0
- `Brain mask generated fast mode` [nonzero_exit]: exit code 1, want 0
- `Brain extraction accurate mode CPU` [nonzero_exit]: exit code 1, want 0
- `Brain extraction with TTA disabled` [nonzero_exit]: exit code 1, want 0
- `Brain extraction with postprocessing enabled` [nonzero_exit]: exit code 1, want 0
- `Brain extraction with postprocessing disabled` [nonzero_exit]: exit code 1, want 0
- `Save both brain and mask` [nonzero_exit]: exit code 1, want 0
- `Do not save mask` [nonzero_exit]: exit code 1, want 0
- `Brain extraction T2 weighted image` [nonzero_exit]: exit code 1, want 0
- `T2 brain mask generated` [nonzero_exit]: exit code 1, want 0
- `Overwrite existing output` [nonzero_exit]: exit code 1, want 0
- `Skip existing output` [nonzero_exit]: exit code 1, want 0
- `Error on missing input` [nonzero_exit]: exit code 1, want 0
- `Full options fast mode` [nonzero_exit]: exit code 1, want 0
- `Full options accurate mode` [nonzero_exit]: exit code 1, want 0

## ilastik

- Passed: 71
- Failed: 17
- Skipped: 0

- `Version check` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --version 2>&1']' timed out after 120.0 seconds
- `Help message` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows headless option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows project option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows workflow option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows new_project option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows readonly option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows debug option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows logfile option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows configfile option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows redirect_output option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows exit_on_failure option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows neural network device option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Help shows tiktorch option` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --help 2>&1']' timed out after 120.0 seconds
- `Headless mode without project error` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --headless 2>&1 || true']' timed out after 120.0 seconds
- `Missing project file error` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --headless --project nonexistent.ilp 2>&1 || true']' timed out after 120.0 seconds
- `New project requires workflow` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-ilastik/.pyneurodesk-fulltest-activate.sh\nrun_ilastik.sh --headless --new_project test.ilp 2>&1 || true']' timed out after 120.0 seconds

## lashis

- Passed: 121
- Failed: 4
- Skipped: 0

- `ANTSPATH environment variable` [missing_output]: missing output fragment '/opt/ants-2.3.0'
- `ASHS_ROOT environment variable` [missing_output]: missing output fragment '/opt/ashs-2.0.0'
- `PATH includes ANTs` [missing_output]: missing output fragment '/opt/ants-2.3.0'
- `PATH includes ASHS` [missing_output]: missing output fragment '/opt/ashs-2.0.0/bin'

## laynii

- Passed: 56
- Failed: 3
- Skipped: 0

- `LN2_RIMIFY conversion` [command_not_found]: exit code 127, want 0
- `LN2_CONNECTED_CLUSTERS labeling` [command_not_found]: exit code 127, want 0
- `LN2_MULTILATERATE columns` [nonzero_exit]: exit code 255, want 139

## lcmodel

- Passed: 97
- Failed: 4
- Skipped: 0

- `LCModel responds to input` [missing_output]: missing output fragment 'FATAL ERROR'
- `Makebasis responds to input` [missing_output]: missing output fragment 'runtime error'
- `Plotraw responds to input` [missing_output]: missing output fragment 'Trailer'
- `KECC responds to input` [missing_output]: missing output fragment 'runtime error'

## lesymap

- Passed: 78
- Failed: 2
- Skipped: 0

- `Rscript file execution` [missing_output]: missing output fragment 'Script executed successfully'
- `R BATCH mode` [missing_output]: missing output fragment 'BATCH mode test successful'

## linda

- Passed: 52
- Failed: 2
- Skipped: 0

- `Rscript file execution` [missing_output]: missing output fragment 'Script executed successfully'
- `R BATCH mode` [missing_output]: missing output fragment 'BATCH mode test successful'

## matlab

- Passed: 23
- Failed: 47
- Skipped: 0

- `MATLAB binary exists` [nonzero_exit]: exit code 1, want 0
- `MATLAB glnxa64 binary exists` [nonzero_exit]: exit code 1, want 0
- `MEX binary exists` [nonzero_exit]: exit code 1, want 0
- `MATLABWindow binary exists` [nonzero_exit]: exit code 1, want 0
- `VersionInfo.xml exists` [nonzero_exit]: exit code 1, want 0
- `Bin directory exists` [nonzero_exit]: exit code 1, want 0
- `Toolbox directory exists` [nonzero_exit]: exit code 1, want 0
- `Extern directory exists` [nonzero_exit]: exit code 1, want 0
- `Sys directory exists` [nonzero_exit]: exit code 1, want 0
- `Java directory exists` [nonzero_exit]: exit code 1, want 0
- `Runtime directory exists` [nonzero_exit]: exit code 1, want 0
- `MATLAB core toolbox` [nonzero_exit]: exit code 1, want 0
- `Image Processing Toolbox` [nonzero_exit]: exit code 1, want 0
- `Signal Processing Toolbox` [nonzero_exit]: exit code 1, want 0
- `Statistics Toolbox` [nonzero_exit]: exit code 1, want 0
- `Deep Learning Toolbox` [nonzero_exit]: exit code 1, want 0
- `Parallel Computing Toolbox` [nonzero_exit]: exit code 1, want 0
- `Computer Vision Toolbox` [nonzero_exit]: exit code 1, want 0
- `Bioinformatics Toolbox` [nonzero_exit]: exit code 1, want 0
- `Text Analytics Toolbox` [nonzero_exit]: exit code 1, want 0
- `Simulink` [nonzero_exit]: exit code 1, want 0
- `Shared toolbox directory` [nonzero_exit]: exit code 1, want 0
- `Local toolbox directory` [nonzero_exit]: exit code 1, want 0
- `Toolbox count` [missing_output]: missing output fragment 'Toolbox count OK'
- `pathdef.m exists` [nonzero_exit]: exit code 1, want 0
- `matlabrc.m exists` [nonzero_exit]: exit code 1, want 0
- `Contents.m in core toolbox` [nonzero_exit]: exit code 1, want 0
- `Shared library count` [missing_output]: missing output fragment 'Library count OK'
- `libmwfl.so exists` [nonzero_exit]: exit code 1, want 0
- `libmwmcr.so exists` [nonzero_exit]: exit code 1, want 0
- `libmx.so exists` [nonzero_exit]: exit code 1, want 0
- `libmex.so exists` [nonzero_exit]: exit code 1, want 0
- `libmat.so exists` [nonzero_exit]: exit code 1, want 0
- `libeng.so exists` [nonzero_exit]: exit code 1, want 0
- `Platform glnxa64 directory` [nonzero_exit]: exit code 1, want 0
- `Runtime libmwmclmcrrt.so` [nonzero_exit]: exit code 123, want 0
- `Java engine directory` [nonzero_exit]: exit code 1, want 0
- `Python engine directory` [nonzero_exit]: exit code 1, want 0
- `MEX include directory` [nonzero_exit]: exit code 1, want 0
- `MEX matrix header` [nonzero_exit]: exit code 1, want 0
- `MEX options directory` [nonzero_exit]: exit code 1, want 0
- `matlab startup script` [nonzero_exit]: exit code 1, want 0
- `glnxa64 architecture check` [nonzero_exit]: exit code 1, want 0
- `Boost libraries` [missing_output]: missing output fragment 'Boost libs not present (expected)'
- `TBB library` [nonzero_exit]: exit code 123, want 0
- `HDF5 library` [nonzero_exit]: exit code 123, want 0
- `zlib library` [nonzero_exit]: exit code 123, want 0

## mfcsc

- Passed: 9
- Failed: 15
- Skipped: 16

- `Verify FC test data exists` [missing_file]: exit code 2, want 0
- `Verify SC test data exists` [missing_file]: exit code 2, want 0
- `Verify FC_SC_LIST file format` [missing_file]: exit code 1, want 0
- `Validate FC matrix format` [missing_file]: exit code 1, want 0
- `Validate SC matrix format` [missing_file]: exit code 1, want 0
- `Basic mfcsc analysis (ipsilateral)` [missing_file]: exit code 1, want 0
- `mfcsc analysis with contralateral connections` [missing_file]: exit code 1, want 0
- `mfcsc with custom not_in_mask_value (0)` [missing_file]: exit code 1, want 0
- `mfcsc keeping negative FC values` [missing_file]: exit code 1, want 0
- `mfcsc with symmetrical output` [missing_file]: exit code 1, want 249
- `mfcsc with all optional parameters` [missing_file]: exit code 1, want 0
- `mfcsc contralateral with negative FC` [missing_file]: exit code 1, want 0
- `Reproducibility test - run 1` [missing_file]: exit code 1, want 0
- `Reproducibility test - run 2` [missing_file]: exit code 1, want 0
- `Single subject analysis` [missing_file]: exit code 1, want 249

## mgltools

- Passed: 72
- Failed: 14
- Skipped: 0

- `summarize_docking.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `summarize_docking.py rmsd option` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `summarize_results4.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `summarize_results41.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `summarize_docking_directory.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `write_conformations_from_dlg.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `write_lowest_energy_ligand.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `write_each_cluster_LE_conf.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `write_all_complexes.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `compute_rms_between_conformations.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `compute_interatomic_distance_per_pose.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `process_VinaResult.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `compute_interatomic_distance_per_vina_pose.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...
- `write_clustering_histogram_postscript.py help` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-mgltools/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-5176d7e243eafb83 -- bash -lc 'pythonsh /opt/mgltools/MGLToolsPckg...

## micapipe

- Passed: 104
- Failed: 16
- Skipped: 0

- `FreeSurfer mri_convert format conversion` [command_not_found]: exit code 255, want 0
- `FreeSurfer mri_convert reorient` [command_not_found]: exit code 255, want 0
- `FreeSurfer mri_convert resample` [command_not_found]: exit code 255, want 0
- `FreeSurfer mri_convert conform` [command_not_found]: exit code 255, want 0
- `FreeSurfer mri_binarize` [command_not_found]: exit code 255, want 0
- `FreeSurfer mri_vol2vol identity` [command_not_found]: exit code 127, want 0
- `ANTs CreateImage blank` [nonzero_exit]: exit code 255, want 134
- `Workbench version` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.sh\nwb_command -version 2>&1 | head -5']' timed out after 120.0 seconds
- `Workbench list commands` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.sh\nwb_command -list-commands 2>&1 | head -20']' timed out after 120.0 seconds
- `Workbench volume info` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.sh\nwb_command -volume-stats ds000001/sub-01/anat/sub-01_T1w.nii.gz -reduce MEAN 2>&1']' timed out ...
- `Workbench volume math` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.sh\nwb_command -volume-math 'x * 2' test_output/t1w_doubled_wb.nii.gz -var x ds000001/sub-01/anat/s...
- `Workbench volume smoothing` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.sh\nwb_command -volume-smoothing ds000001/sub-01/anat/sub-01_T1w.nii.gz 2 test_output/t1w_smooth_wb...
- `Workbench volume dilate` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.sh\nwb_command -volume-dilate test_output/t1w_brain_mask.nii.gz 2 NEAREST test_output/mask_dilate_w...
- `Workbench volume resample` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.sh\nwb_command -volume-resample ds000001/sub-01/anat/sub-01_T1w.nii.gz ds000001/sub-01/anat/sub-01_...
- `dcm2niix version` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-micapipe/.pyneurodesk-fulltest-activate.sh\ndcm2niix --version 2>&1']' timed out after 120.0 seconds
- `Format conversion roundtrip` [nonzero_exit]: exit code 255, want 0

## minc

- Passed: 0
- Failed: 5
- Skipped: 106

- `Version check (mincinfo)` [missing_output]: missing output fragment 'program'
- `Convert T1w NIfTI to MINC` [nonzero_exit]: exit code 1, want 0
- `Convert T2 NIfTI to MINC` [nonzero_exit]: exit code 1, want 0
- `Convert BOLD NIfTI to MINC` [nonzero_exit]: exit code 1, want 0
- `ANTs version check` [missing_output]: missing output fragment 'ANTs'

## mricron

- Passed: 101
- Failed: 2
- Skipped: 0

- `pigz stdout mode` [nonzero_exit]: exit code 1, want 0
- `Resources directory in PATH` [missing_output]: missing output fragment '2'

## mritools

- Passed: 16
- Failed: 3
- Skipped: 35

- `Create synthetic phase image from T1w` [missing_file]: exit code 2, want 0
- `Create synthetic magnitude for testing` [missing_file]: exit code 2, want 0
- `Create 4D synthetic phase from BOLD` [missing_file]: exit code 2, want 0

## mrsiproc

- Passed: 99
- Failed: 8
- Skipped: 0

- `MRSI_Reconstruction responds without input` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-d0c78563622eff5a -- bash -lc \'env LD_LIBRARY_PATH=/opt/MATLAB_Ru...
- `extract_met_maps binary exists` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-d0c78563622eff5a -- bash -lc \'env LD_LIBRARY_PATH=/opt/MATLAB_Ru...
- `extract_spectra binary exists` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-d0c78563622eff5a -- bash -lc \'env LD_LIBRARY_PATH=/opt/MATLAB_Ru...
- `CreateSpectralNiftiMap binary exists` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-d0c78563622eff5a -- bash -lc \'env LD_LIBRARY_PATH=/opt/MATLAB_Ru...
- `GetPar_CreateTempl_MaskPart1 binary exists` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-d0c78563622eff5a -- bash -lc \'env LD_LIBRARY_PATH=/opt/MATLAB_Ru...
- `julia_write_lcm_files binary exists` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-d0c78563622eff5a -- bash -lc \'env LD_LIBRARY_PATH=/opt/MATLAB_Ru...
- `segmentation_simple binary exists` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-mrsiproc/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-d0c78563622eff5a -- bash -lc \'env LD_LIBRARY_PATH=/opt/MATLAB_Ru...
- `FAST tissue segmentation` [nonzero_exit]: exit code 255, want 0

## mrtrix3tissue

- Passed: 114
- Failed: 3
- Skipped: 0

- `mrfilter FFT forward` [nonzero_exit]: exit code 0, want 8
- `Pipeline Z-score normalization` [nonzero_exit]: exit code 1, want 0
- `Pipeline intensity normalization` [nonzero_exit]: exit code 1, want 0

## networkcorrespondancetoolkit

- Passed: 55
- Failed: 7
- Skipped: 1

- `Numpy save array` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-networkcorrespondancetoolkit/.pyneurodesk-fulltest-activate.sh\npython3 -c "\nimport numpy as np\nimport nibabel as nib\nimg = nib.load(\'d...
- `Scipy ndimage operations` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-networkcorrespondancetoolkit/.pyneurodesk-fulltest-activate.sh\npython3 -c "\nimport numpy as np\nimport nibabel as nib\nfrom scipy import ...
- `Scipy statistical functions` [missing_output]: missing output fragment 'Skewness:'
- `Pandas DataFrame operations` [other]: missing output /home/runner/work/_temp/pyneurodesk-fulltest-networkcorrespondancetoolkit/test_output/network_results.csv
- `Sklearn PCA` [nonzero_exit]: exit code 255, want 0
- `Large array handling` [nonzero_exit]: exit code 255, want 0
- `Handle missing file gracefully` [missing_output]: missing output fragment 'Correctly raised'

## neurodock

- Passed: 111
- Failed: 6
- Skipped: 0

- `PyDesigner version check` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate.sh\npydesigner --version 2>&1']' timed out after 120.0 seconds
- `PyDesigner help` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate.sh\npydesigner --help 2>&1']' timed out after 120.0 seconds
- `PyDesigner advanced options` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate.sh\npydesigner --adv --help 2>&1']' timed out after 120.0 seconds
- `DIPY dipy_buan_lmm help` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate.sh\ndipy_buan_lmm --help 2>&1']' timed out after 120.0 seconds
- `DIPY dipy_buan_profiles help` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate.sh\ndipy_buan_profiles --help 2>&1']' timed out after 120.0 seconds
- `DIPY dipy_buan_shapes help` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-neurodock/.pyneurodesk-fulltest-activate.sh\ndipy_buan_shapes --help 2>&1']' timed out after 120.0 seconds

## nftsim

- Passed: 47
- Failed: 20
- Skipped: 0

- `rTMS plasticity` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-nftsim/.pyneurodesk-fulltest-activate.sh\nnftsim -i /app/configs/Plasticity/rtms.conf -o test_output/plasticity-rtms.output']' timed out af...
- `Verbose output mode` [missing_output]: missing output fragment 'Time:'
- `Verify output column headers` [missing_output]: missing output fragment 'Time'
- `Verify population parameters in output` [missing_output]: missing output fragment 'Population'
- `Verify propagator parameters in output` [missing_output]: missing output fragment 'Propagator'
- `Verify coupling parameters in output` [missing_output]: missing output fragment 'Coupling'
- `Verify node count in EIRS` [missing_output]: missing output fragment '144'
- `Verify node count in E cortical` [missing_output]: missing output fragment '900'
- `Verify node count in EI cortical` [missing_output]: missing output fragment '2048'
- `Verify time step in EIRS` [missing_output]: missing output fragment 'Deltat:'
- `Verify simulation duration in EIRS` [missing_output]: missing output fragment '15'
- `Verify sigmoid firing function` [missing_output]: missing output fragment 'Sigmoid'
- `Verify firing parameters` [missing_output]: missing output fragment 'Qmax'
- `Verify white noise stimulus` [missing_output]: missing output fragment 'White'
- `Verify pulse stimulus in stimuli-only` [missing_output]: missing output fragment 'Pulse'
- `Verify wave propagator` [missing_output]: missing output fragment 'Wave'
- `Verify map propagator` [missing_output]: missing output fragment 'Map'
- `Verify dendrite parameters` [missing_output]: missing output fragment 'Dendrite'
- `Verify alpha parameter` [missing_output]: missing output fragment 'alpha:'
- `Verify beta parameter` [missing_output]: missing output fragment 'beta:'

## nibabies

- Passed: 63
- Failed: 7
- Skipped: 0

- `NiBabies help output` [missing_output]: missing output fragment 'NiBabies'
- `BIDS validator help` [missing_output]: missing output fragment 'bids-validator'
- `FSL header dump` [missing_output]: missing output fragment 'sizeof_hdr'
- `ANTs denoise image` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-nibabies/.pyneurodesk-fulltest-activate.sh\nDenoiseImage -d 3 -i ds000001/sub-01/anat/sub-01_T1w.nii.gz -o test_output/ants/t1w_denoised.ni...
- `ANTs motion correction` [command_not_found]: exit code 127, want 0
- `FreeSurfer mri_convert help` [missing_output]: missing output fragment 'mri_convert'
- `FreeSurfer mri_info` [missing_output]: missing output fragment 'dimensions'

## niistat

- Passed: 92
- Failed: 1
- Skipped: 0

- `Octave version check` [missing_output]: missing output fragment 'GNU Octave'

## oshyx

- Passed: 39
- Failed: 73
- Skipped: 5

- `OSHy.py version and banner` [missing_output]: missing output fragment 'OSHy-X v0.4'
- `OSHy.py help message` [command_not_found]: exit code 127, want 0
- `OSHy.py help contains all options` [command_not_found]: exit code 127, want 0
- `OSHy.py crop option documentation` [command_not_found]: exit code 127, want 0
- `OSHy.py weighting option documentation` [command_not_found]: exit code 127, want 0
- `OSHy.py denoise option documentation` [command_not_found]: exit code 127, want 0
- `OSHy.py field correction option documentation` [command_not_found]: exit code 127, want 0
- `OSHy.py mosaic option documentation` [command_not_found]: exit code 127, want 0
- `OSHy.py tesla option documentation` [command_not_found]: exit code 127, want 0
- `OSHy.py bimodal option documentation` [command_not_found]: exit code 127, want 0
- `OSHy.py threads option documentation` [command_not_found]: exit code 127, want 0
- `OSHy.py copyright notice` [command_not_found]: exit code 127, want 0
- `OSHy.py missing arguments message` [command_not_found]: exit code 127, want 0
- `Test T1w image exists` [command_not_found]: exit code 127, want 0
- `Test T2w image exists` [command_not_found]: exit code 127, want 0
- `3T template exists` [command_not_found]: exit code 127, want 0
- `7T template exists` [command_not_found]: exit code 127, want 0
- `3T bounding box exists` [command_not_found]: exit code 127, want 0
- `7T bounding box exists` [command_not_found]: exit code 127, want 0
- `3T atlases directory exists` [command_not_found]: exit code 127, want 0
- `7T atlases directory exists` [command_not_found]: exit code 127, want 0
- `3T atlas count` [command_not_found]: exit code 127, want 0
- `7T atlas count` [command_not_found]: exit code 127, want 0
- `ANTsPy image read` [command_not_found]: exit code 127, want 0
- `ANTsPy image dimensions` [command_not_found]: exit code 127, want 0
- `ANTsPy image spacing` [command_not_found]: exit code 127, want 0
- `ANTsPy denoise function` [command_not_found]: exit code 127, want 0
- `nibabel load test image` [command_not_found]: exit code 127, want 0
- `ANTsPy image write` [command_not_found]: exit code 127, want 0
- `ANTsPy image smoothing` [command_not_found]: exit code 127, want 0
- `ANTsPy image thresholding` [command_not_found]: exit code 127, want 0
- `ANTsPy Otsu thresholding` [command_not_found]: exit code 127, want 0
- `ANTsPy image resampling` [command_not_found]: exit code 127, want 0
- `ANTsPy N4 bias correction` [command_not_found]: exit code 127, want 0
- `ANTsPy histogram matching` [command_not_found]: exit code 127, want 0
- `ANTsPy iMath operations gradient` [command_not_found]: exit code 127, want 0
- `ANTsPy iMath operations Laplacian` [command_not_found]: exit code 127, want 0
- `3T template dimensions` [command_not_found]: exit code 127, want 0
- `7T template dimensions` [command_not_found]: exit code 127, want 0
- `3T bounding box is binary` [command_not_found]: exit code 127, want 0
- `7T bounding box is binary` [command_not_found]: exit code 127, want 0
- `3T atlas labels check` [command_not_found]: exit code 127, want 0
- `7T atlas labels check` [command_not_found]: exit code 127, want 0
- `ANTsPy affine registration` [command_not_found]: exit code 127, want 0
- `ANTsPy transform application` [command_not_found]: exit code 127, want 0
- `ANTsPy label geometry measures` [command_not_found]: exit code 127, want 0
- `ImageMath label stats` [command_not_found]: exit code 127, want 0
- `Read compressed NIfTI` [command_not_found]: exit code 127, want 0
- `Write uncompressed NIfTI` [command_not_found]: exit code 127, want 0
- `Header information extraction` [command_not_found]: exit code 127, want 0
- `Image orientation check` [command_not_found]: exit code 127, want 0
- `Image direction matrix` [command_not_found]: exit code 127, want 0
- `glob available` [command_not_found]: exit code 127, want 0
- `OSHy module import` [command_not_found]: exit code 127, want 0
- `convert_to_bool True` [command_not_found]: exit code 127, want 0
- `convert_to_bool False` [command_not_found]: exit code 127, want 0
- `convert_to_bool case insensitive` [command_not_found]: exit code 127, want 0
- `OSHy_data class instantiation` [command_not_found]: exit code 127, want 0
- `OSHy_data get_template` [command_not_found]: exit code 127, want 0
- `OSHy_data get_template_box` [command_not_found]: exit code 127, want 0
- `OSHy_data get_atlases 3T T1w` [command_not_found]: exit code 127, want 0
- `OSHy_data get_atlases 7T T1w` [command_not_found]: exit code 127, want 0
- `OSHy_data get_labels` [command_not_found]: exit code 127, want 0
- `ANTsPy crop image` [command_not_found]: exit code 127, want 0
- `ANTsPy pad image` [command_not_found]: exit code 127, want 0
- `ANTsPy slice image` [command_not_found]: exit code 127, want 0
- `ANTsPy get mask` [command_not_found]: exit code 127, want 0
- `ANTsPy image clone` [command_not_found]: exit code 127, want 0
- `ANTsPy reflect image` [command_not_found]: exit code 127, want 0
- `ANTsPy multiply images` [command_not_found]: exit code 127, want 0
- `ANTsPy copy image info` [command_not_found]: exit code 127, want 0
- `ANTsPy image similarity` [command_not_found]: exit code 127, want 0
- `ANTsPy rank intensity` [command_not_found]: exit code 127, want 0

## osprey

- Passed: 88
- Failed: 3
- Skipped: 0

- `ospreyCMD shows help (no args)` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-osprey/.pyneurodesk-fulltest-activate.sh\nospreyCMD 2>&1 || true']' timed out after 120.0 seconds
- `T1w structural image readable (sub-01)` [nonzero_exit]: exit code 1, want 0
- `T1w structural image readable (sub-02)` [nonzero_exit]: exit code 1, want 0

## ospreybids

- Passed: 101
- Failed: 6
- Skipped: 0

- `LD_LIBRARY_PATH includes MCR` [missing_output]: missing output fragment '/mcr_path/v912'
- `BASIS_SETS_PATH environment variable` [missing_output]: missing output fragment '/HBCD_basissets'
- `EXECUTABLE_PATH environment variable` [missing_output]: missing output fragment '/code/run_compiled.sh'
- `MCR_PATH environment variable` [missing_output]: missing output fragment '/mcr_path/v912'
- `DEPLOY_BINS environment variable` [missing_output]: missing output fragment 'osprey'
- `OspreyHBCD job file error` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-ospreybids/.pyneurodesk-fulltest-activate.sh\nneurodesk shell exec fulltest-4d557573be6fb9b9 -- bash -lc '/code/OspreyHBCD 2>&1 || true'"]'...

## palm

- Passed: 15
- Failed: 40
- Skipped: 0

- `Basic input loading` [nonzero_exit]: exit code 1, want 0
- `Multiple inputs` [nonzero_exit]: exit code 1, want 0
- `One-sample t-test` [nonzero_exit]: exit code 1, want 0
- `One-sample with save1-p` [nonzero_exit]: exit code 1, want 0
- `Two-sample t-test (unpaired)` [nonzero_exit]: exit code 1, want 0
- `Two-sample with variance groups` [nonzero_exit]: exit code 1, want 0
- `Two-sample with auto variance groups` [nonzero_exit]: exit code 1, want 0
- `Two-tailed t-test` [nonzero_exit]: exit code 1, want 0
- `Sign-flipping test` [nonzero_exit]: exit code 1, want 0
- `Combined EE and ISE` [nonzero_exit]: exit code 1, want 0
- `F-test` [nonzero_exit]: exit code 1, want 0
- `F-test only` [nonzero_exit]: exit code 1, want 0
- `Within-block permutation` [nonzero_exit]: exit code 1, want 0
- `Whole-block permutation` [nonzero_exit]: exit code 1, want 0
- `FDR correction` [nonzero_exit]: exit code 1, want 0
- `Negative binomial acceleration` [nonzero_exit]: exit code 1, want 0
- `Tail approximation acceleration` [nonzero_exit]: exit code 1, want 0
- `Gamma approximation acceleration` [nonzero_exit]: exit code 1, want 0
- `No permutation approximation` [nonzero_exit]: exit code 1, want 0
- `Save 1-p values` [nonzero_exit]: exit code 1, want 0
- `Log p-values` [nonzero_exit]: exit code 1, want 0
- `Save DOF` [nonzero_exit]: exit code 1, want 0
- `Save GLM outputs` [nonzero_exit]: exit code 1, want 0
- `Save effective mask` [nonzero_exit]: exit code 1, want 0
- `Save permutation metrics` [nonzero_exit]: exit code 1, want 0
- `Save parametric p-values` [nonzero_exit]: exit code 1, want 0
- `Fixed seed for reproducibility` [nonzero_exit]: exit code 1, want 0
- `Exhaustive permutations` [nonzero_exit]: exit code 1, want 0
- `Freedman-Lane method` [nonzero_exit]: exit code 1, want 0
- `Dekker method` [nonzero_exit]: exit code 1, want 0
- `ter Braak method` [nonzero_exit]: exit code 1, want 0
- `Inverse normal transformation` [nonzero_exit]: exit code 1, want 0
- `Inverse normal Blom method` [nonzero_exit]: exit code 1, want 0
- `Quiet mode` [nonzero_exit]: exit code 1, want 0
- `Guttman partition method` [nonzero_exit]: exit code 1, want 0
- `Beckmann partition method` [nonzero_exit]: exit code 1, want 0
- `Double precision` [nonzero_exit]: exit code 1, want 0
- `Synchronized permutations` [nonzero_exit]: exit code 1, want 0
- `Verbose filenames` [nonzero_exit]: exit code 1, want 0
- `Save maximum statistic distribution` [nonzero_exit]: exit code 1, want 0

## pydeface

- Passed: 57
- Failed: 2
- Skipped: 0

- `Check default template exists` [missing_file]: exit code 2, want 0
- `Check default facemask exists` [missing_file]: exit code 2, want 0

## qmrlab

- Passed: 48
- Failed: 2
- Skipped: 0

- `Octave version check` [missing_output]: missing output fragment 'GNU Octave'
- `AMICO model check` [nonzero_exit]: exit code 1, want 0

## qsiprep

- Passed: 81
- Failed: 9
- Skipped: 0

- `MRtrix3 mask filter - dilate` [command_not_found]: exit code 127, want 0
- `MRtrix3 mask filter - erode` [command_not_found]: exit code 127, want 0
- `MRtrix3 mask filter - median` [command_not_found]: exit code 127, want 0
- `MRtrix3 mask filter - clean` [command_not_found]: exit code 127, want 0
- `MRtrix3 mask filter - connected components` [command_not_found]: exit code 127, want 0
- `MRtrix3 dwi2fod help` [command_not_found]: exit code 127, want 0
- `MRtrix3 tckgen help` [command_not_found]: exit code 127, want 0
- `ANTs CreateImage` [nonzero_exit]: exit code 255, want 134
- `MRtrix3 pipeline - mask and apply` [nonzero_exit]: exit code 1, want 0

## qsmxt

- Passed: 61
- Failed: 11
- Skipped: 1

- `DenoiseImage basic` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh\nDenoiseImage -d 3 -i ds000001/sub-01/anat/sub-01_T1w.nii.gz -o test_output/t1w_denoised.nii.gz']' ...
- `DenoiseImage with noise image` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh\nDenoiseImage -d 3 -i ds000001/sub-01/anat/sub-01_T1w.nii.gz -o [test_output/t1w_denoised2.nii.gz,t...
- `Julia ROMEO package` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh\njulia -e \'using ROMEO; println("ROMEO loaded successfully")\'']' timed out after 120.0 seconds
- `Julia QSM package` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh\njulia -e \'using QSM; println("QSM loaded successfully")\' 2>&1']' timed out after 120.0 seconds
- `Julia MriResearchTools package` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh\njulia -e \'using MriResearchTools; println("MriResearchTools loaded successfully")\'']' timed out ...
- `Julia QuantitativeSusceptibilityMappingTGV package` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh\njulia -e \'using QuantitativeSusceptibilityMappingTGV; println("TGV-QSM loaded successfully")\'']'...
- `Julia CLEARSWI package` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh\njulia -e \'using CLEARSWI; println("CLEARSWI loaded successfully")\'']' timed out after 120.0 seconds
- `JULIA_DEPOT_PATH set` [missing_output]: missing output fragment 'julia_depot'
- `FASTSURFER_HOME set` [missing_output]: missing output fragment 'FastSurfer'
- `T1w preprocessing pipeline` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh\nDenoiseImage -d 3 -i ds000001/sub-01/anat/sub-01_T1w.nii.gz -o test_output/t1w_pipeline_denoised.n...
- `Multi-modal registration pipeline` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-qsmxt/.pyneurodesk-fulltest-activate.sh\nantsRegistration -d 3 \\\n  -m MI[ds000001/sub-01/anat/sub-01_T1w.nii.gz,ds000001/sub-01/anat/sub-...

## rabies

- Passed: 36
- Failed: 39
- Skipped: 1

- `RABIES help` [missing_output]: missing output fragment 'RABIES performs multiple stages'
- `RABIES preprocess help` [missing_output]: missing output fragment 'rabies preprocess'
- `RABIES confound_correction help` [missing_output]: missing output fragment 'rabies confound_correction'
- `RABIES analysis help` [missing_output]: missing output fragment 'rabies analysis'
- `Error check utility help` [missing_output]: missing output fragment 'Parser to handle testing'
- `fslmaths help` [missing_output]: missing output fragment 'fslmaths'
- `fslmaths mean operation` [nonzero_exit]: exit code 1, want 0
- `fslmaths smoothing` [nonzero_exit]: exit code 1, want 0
- `fslmaths threshold and binarize` [nonzero_exit]: exit code 1, want 0
- `fslmaths bandpass filter` [nonzero_exit]: exit code 1, want 0
- `nibabel import` [nonzero_exit]: exit code 1, want 0
- `nilearn import` [nonzero_exit]: exit code 1, want 0
- `nipype import` [nonzero_exit]: exit code 1, want 0
- `scipy import` [nonzero_exit]: exit code 1, want 0
- `sklearn import` [nonzero_exit]: exit code 1, want 0
- `pandas import` [nonzero_exit]: exit code 1, want 0
- `matplotlib import` [nonzero_exit]: exit code 1, want 0
- `rabies import` [nonzero_exit]: exit code 1, want 0
- `Load NIfTI with nibabel` [nonzero_exit]: exit code 1, want 0
- `nilearn smoothing` [nonzero_exit]: exit code 1, want 0
- `nilearn mean image` [nonzero_exit]: exit code 1, want 0
- `nilearn resample to template` [nonzero_exit]: exit code 1, want 0
- `nilearn masking` [nonzero_exit]: exit code 1, want 0
- `nipype ANTs interface` [nonzero_exit]: exit code 1, want 0
- `nipype FSL interface` [nonzero_exit]: exit code 1, want 0
- `nipype AFNI interface` [nonzero_exit]: exit code 1, want 0
- `antsMotionCorr on BOLD subset` [command_not_found]: exit code 127, want 0
- `fslmaths statistics` [nonzero_exit]: exit code 1, want 0
- `Compute framewise displacement` [nonzero_exit]: exit code 1, want 0
- `Compute DVARS` [nonzero_exit]: exit code 1, want 0
- `nilearn connectivity matrix` [nonzero_exit]: exit code 1, want 0
- `MELODIC availability` [missing_output]: missing output fragment 'MELODIC'
- `fMRI preprocessing chain` [command_not_found]: exit code 127, want 0
- `Simulate confound regression` [nonzero_exit]: exit code 1, want 0
- `Bandpass filter simulation` [nonzero_exit]: exit code 1, want 0
- `Save as float32` [nonzero_exit]: exit code 1, want 0
- `fslmaths output types` [nonzero_exit]: exit code 1, want 0
- `Check multiprocessing` [nonzero_exit]: exit code 1, want 0
- `nipype plugin check` [nonzero_exit]: exit code 1, want 0

## romeo

- Passed: 97
- Failed: 7
- Skipped: 0

- `Unwrap synthetic phase basic` [nonzero_exit]: exit code 1, want 0
- `Unwrap synthetic phase with magnitude` [nonzero_exit]: exit code 1, want 0
- `Unwrap synthetic phase with custom mask` [nonzero_exit]: exit code 1, want 0
- `Multi-echo temporal unwrapping` [nonzero_exit]: exit code 1, want 0
- `Multi-echo B0 calculation` [nonzero_exit]: exit code 1, want 0
- `Multi-echo individual unwrapping` [nonzero_exit]: exit code 1, want 0
- `Multi-echo phase offset correction` [nonzero_exit]: exit code 1, want 0

## root

- Passed: 83
- Failed: 8
- Skipped: 10

- `Create ROOT file with histogram` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-root/.pyneurodesk-fulltest-activate.sh\nroot -b -l -q -e \'TFile f("test_output/test_hist.root","RECREATE"); TH1F h("h1","histogram",100,-5...
- `Create ROOT file with tree` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-root/.pyneurodesk-fulltest-activate.sh\nroot -b -l -q -e \'TFile f("test_output/test_tree.root","RECREATE"); TTree t("tree","sample tree");...
- `hadd merge files` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-root/.pyneurodesk-fulltest-activate.sh\nroot -b -l -q -e \'TFile f("test_output/merge1.root","RECREATE"); TH1F h("h","h",10,0,10); h.Fill(5...
- `TH1F Gaussian fit` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-root/.pyneurodesk-fulltest-activate.sh\nroot -b -l -q -e \'TH1F h("h","test",100,-5,5); h.FillRandom("gaus",10000); h.Fit("gaus","Q"); TF1 ...
- `Save histogram to PNG` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-root/.pyneurodesk-fulltest-activate.sh\nroot -b -l -q -e \'TCanvas c("c","c",800,600); TH1F h("h","histogram",100,-5,5); h.FillRandom("gaus...
- `Save histogram to PDF` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-root/.pyneurodesk-fulltest-activate.sh\nroot -b -l -q -e \'TCanvas c("c","c",800,600); TH1F h("h","histogram",100,-5,5); h.FillRandom("gaus...
- `TTree branch types` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-root/.pyneurodesk-fulltest-activate.sh\nroot -b -l -q -e \'TFile f("test_output/multi_branch.root","RECREATE"); TTree t("t","tree"); int i;...
- `TChain multiple files` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-root/.pyneurodesk-fulltest-activate.sh\nroot -b -l -q -e \'TFile f1("test_output/chain1.root","RECREATE"); TTree t("t","t"); int x; t.Branc...

## rstudio

- Passed: 135
- Failed: 3
- Skipped: 0

- `RStudio version check` [missing_output]: missing output fragment '2023.12.1'
- `PNG graphics output` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-rstudio/.pyneurodesk-fulltest-activate.sh\nRscript -e "png(\'test_output/plot.png\'); plot(1:10); dev.off(); print(\'done\')"']' timed out ...
- `ggplot2 save` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-rstudio/.pyneurodesk-fulltest-activate.sh\nRscript -e "library(ggplot2); p <- ggplot(mtcars, aes(wt, mpg)) + geom_point(); ggsave(\'test_ou...

## samsrfx

- Passed: 116
- Failed: 2
- Skipped: 0

- `Deploy bins environment` [missing_output]: missing output fragment 'samsrfx'
- `PATH includes samsrfx` [missing_output]: missing output fragment '/opt/samsrfx-v10.004/'

## sigviewer

- Passed: 22
- Failed: 12
- Skipped: 0

- `Version check` [missing_output]: missing output fragment 'SigViewer 0.6.4'
- `Help information` [missing_output]: missing output fragment 'SigViewer - a biosignal viewer'
- `Help shows file argument` [missing_output]: missing output fragment 'Input file (optional)'
- `Full help with Qt options` [missing_output]: missing output fragment '--platform'
- `Invalid option handling` [missing_output]: missing output fragment 'Unknown option'
- `Offscreen startup test` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-sigviewer/.pyneurodesk-fulltest-activate.sh\ntimeout 5 bash -c 'QT_QPA_PLATFORM=offscreen sigviewer 2>&1' || true\n"]' timed out after 120....
- `Nonexistent file handling` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-sigviewer/.pyneurodesk-fulltest-activate.sh\ntimeout 5 bash -c 'QT_QPA_PLATFORM=offscreen sigviewer /nonexistent/file.edf 2>&1' || true\n"]...
- `Invalid file format handling` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-sigviewer/.pyneurodesk-fulltest-activate.sh\necho "not a valid biosignal file" > test_output/invalid.txt\ntimeout 5 bash -c \'QT_QPA_PLATFO...
- `Multiple help invocations` [missing_output]: missing output fragment 'SigViewer 0.6.4'
- `Empty filename handling` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-sigviewer/.pyneurodesk-fulltest-activate.sh\ntimeout 5 bash -c \'QT_QPA_PLATFORM=offscreen sigviewer "" 2>&1\' || true\n']' timed out after...
- `Directory as input` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-sigviewer/.pyneurodesk-fulltest-activate.sh\ntimeout 5 bash -c 'QT_QPA_PLATFORM=offscreen sigviewer /tmp 2>&1' || true\n"]' timed out after...
- `Permission denied handling` [timeout]: Command '['bash', '-lc', "source /home/runner/work/_temp/pyneurodesk-fulltest-sigviewer/.pyneurodesk-fulltest-activate.sh\ntouch test_output/unreadable.edf\nchmod 000 test_output/unreadable.edf\ntimeout 5 bash -c 'QT_...

## slicer

- Passed: 65
- Failed: 1
- Skipped: 0

- `Slicer launcher help` [timeout]: Command '['bash', '-lc', 'source /home/runner/work/_temp/pyneurodesk-fulltest-slicer/.pyneurodesk-fulltest-activate.sh\nSlicer --launcher-help 2>&1']' timed out after 120.0 seconds

## sovabids

- Passed: 4
- Failed: 89
- Skipped: 0

- `Check sovabids version` [nonzero_exit]: exit code 1, want 0
- `Check sovabids module import` [nonzero_exit]: exit code 1, want 0
- `Check sovabids package location` [nonzero_exit]: exit code 1, want 0
- `Check sovaconvert CLI exists` [nonzero_exit]: exit code 1, want 0
- `Check sovapply CLI exists` [nonzero_exit]: exit code 1, want 0
- `sovaconvert help` [nonzero_exit]: exit code 1, want 0
- `sovapply help` [nonzero_exit]: exit code 1, want 0
- `Import heuristics module` [nonzero_exit]: exit code 1, want 0
- `Import convert module` [nonzero_exit]: exit code 1, want 0
- `Import rules module` [nonzero_exit]: exit code 1, want 0
- `Import bids module` [nonzero_exit]: exit code 1, want 0
- `Import files module` [nonzero_exit]: exit code 1, want 0
- `Import dicts module` [nonzero_exit]: exit code 1, want 0
- `Import parsers module` [nonzero_exit]: exit code 1, want 0
- `Import misc module` [nonzero_exit]: exit code 1, want 0
- `Import schemas module` [nonzero_exit]: exit code 1, want 0
- `Import settings module` [nonzero_exit]: exit code 1, want 0
- `Import errors module` [nonzero_exit]: exit code 1, want 0
- `Import loggers module` [nonzero_exit]: exit code 1, want 0
- `Import datasets module` [nonzero_exit]: exit code 1, want 0
- `Import sovarpc module` [nonzero_exit]: exit code 1, want 0
- `Check supported extensions` [nonzero_exit]: exit code 1, want 0
- `Supported extensions include EDF` [nonzero_exit]: exit code 1, want 0
- `Supported extensions include BDF` [nonzero_exit]: exit code 1, want 0
- `Supported extensions include SET` [nonzero_exit]: exit code 1, want 0
- `Supported extensions include CNT` [nonzero_exit]: exit code 1, want 0
- `Supported extensions include FIF` [nonzero_exit]: exit code 1, want 0
- `Check NULL values constant` [nonzero_exit]: exit code 1, want 0
- `Check SECTION_STRING constant` [nonzero_exit]: exit code 1, want 0
- `Import from_io_example function` [nonzero_exit]: exit code 1, want 0
- `Test from_io_example basic usage` [nonzero_exit]: exit code 1, want 0
- `Import parse_entities_from_bidspath` [nonzero_exit]: exit code 1, want 0
- `Test parse_entities_from_bidspath` [nonzero_exit]: exit code 1, want 0
- `Import parse_path_pattern_from_entities` [nonzero_exit]: exit code 1, want 0
- `Import find_bidsroot function` [nonzero_exit]: exit code 1, want 0
- `Import BIDSValidator class` [nonzero_exit]: exit code 1, want 0
- `Create BIDSValidator instance` [nonzero_exit]: exit code 1, want 0
- `Test BIDSValidator is_bids method` [nonzero_exit]: exit code 1, want 0
- `Import parse_from_placeholder` [nonzero_exit]: exit code 1, want 0
- `Test parse_from_placeholder` [nonzero_exit]: exit code 1, want 0
- `Import parse_from_regex` [nonzero_exit]: exit code 1, want 0
- `Import placeholder_to_regex` [nonzero_exit]: exit code 1, want 0
- `Test placeholder_to_regex conversion` [nonzero_exit]: exit code 1, want 0
- `Import deep_get function` [nonzero_exit]: exit code 1, want 0
- `Import deep_merge function` [nonzero_exit]: exit code 1, want 0
- `Import deep_merge_N function` [nonzero_exit]: exit code 1, want 0
- `Import flatten function` [nonzero_exit]: exit code 1, want 0
- `Import nested_notation_to_tree` [nonzero_exit]: exit code 1, want 0
- `Test nested_notation_to_tree` [nonzero_exit]: exit code 1, want 0
- `Import load_rules function` [nonzero_exit]: exit code 1, want 0
- `Import get_files function` [nonzero_exit]: exit code 1, want 0
- `Import apply_rules function` [nonzero_exit]: exit code 1, want 0
- `Import apply_rules_to_single_file` [nonzero_exit]: exit code 1, want 0
- `Test load_rules with dictionary` [nonzero_exit]: exit code 1, want 0
- `Import convert_them function` [nonzero_exit]: exit code 1, want 0
- `Import sovaconvert function` [nonzero_exit]: exit code 1, want 0
- `Import update_dataset_description` [nonzero_exit]: exit code 1, want 0
- `Import get_dummy_raw function` [nonzero_exit]: exit code 1, want 0
- `Test get_dummy_raw function` [nonzero_exit]: exit code 1, want 0
- `Import make_dummy_dataset function` [nonzero_exit]: exit code 1, want 0
- `Import get_num_digits function` [nonzero_exit]: exit code 1, want 0
- `Test get_num_digits function` [nonzero_exit]: exit code 1, want 0
- `Import flat_paren_counter function` [nonzero_exit]: exit code 1, want 0
- `Import ApplyError exception` [nonzero_exit]: exit code 1, want 0
- `Import ConvertError exception` [nonzero_exit]: exit code 1, want 0
- `Import RulesError exception` [nonzero_exit]: exit code 1, want 0
- `Import FileListError exception` [nonzero_exit]: exit code 1, want 0
- `Import SaveError exception` [nonzero_exit]: exit code 1, want 0
- `Import setup_logging function` [nonzero_exit]: exit code 1, want 0
- `Import get_sova2coin_bidsmap` [nonzero_exit]: exit code 1, want 0
- `Check MNE installation` [nonzero_exit]: exit code 1, want 0
- `Check MNE Raw object creation` [nonzero_exit]: exit code 1, want 0
- `Check MNE-BIDS installation` [nonzero_exit]: exit code 1, want 0
- `Import write_raw_bids from MNE-BIDS` [nonzero_exit]: exit code 1, want 0
- `Import BIDSPath from MNE-BIDS` [nonzero_exit]: exit code 1, want 0
- `Check bids-validator installation` [nonzero_exit]: exit code 1, want 0
- `Check pandas installation` [nonzero_exit]: exit code 1, want 0
- `Check numpy installation` [nonzero_exit]: exit code 1, want 0
- `Check scipy installation` [nonzero_exit]: exit code 1, want 0
- `Check pybv installation` [nonzero_exit]: exit code 1, want 0
- `Check PyYAML installation` [nonzero_exit]: exit code 1, want 0
- `Create dummy raw and check channels` [nonzero_exit]: exit code 1, want 0
- `Create dummy raw and check sampling frequency` [nonzero_exit]: exit code 1, want 0
- `Create dummy raw and check duration` [nonzero_exit]: exit code 1, want 0
- `Test full from_io_example workflow` [nonzero_exit]: exit code 1, want 0
- `Test parse_entities with run` [nonzero_exit]: exit code 1, want 0
- `Test parse_entities with acquisition` [nonzero_exit]: exit code 1, want 0
- `Test deep_merge with nested dicts` [nonzero_exit]: exit code 1, want 0
- `Test deep_get with path` [nonzero_exit]: exit code 1, want 0

## spm12

- Passed: 120
- Failed: 4
- Skipped: 4

- `Create smoothing batch file` [missing_output]: missing output fragment 'matlabbatch'
- `Create imcalc batch file` [missing_output]: missing output fragment 'imcalc'
- `Create imcalc threshold batch` [missing_output]: missing output fragment 'imcalc'
- `SPM12 read image data` [missing_output]: missing output fragment 'Mean:'

## trackvis

- Passed: 7
- Failed: 72
- Skipped: 0

- `Load track file (no render)` [nonzero_exit]: exit code 1, want 0
- `Display track count` [nonzero_exit]: exit code 1, want 0
- `Length threshold (minimum only)` [nonzero_exit]: exit code 1, want 0
- `Length threshold (range)` [nonzero_exit]: exit code 1, want 0
- `Length threshold output` [nonzero_exit]: exit code 1, want 0
- `Sagittal slice filter` [nonzero_exit]: exit code 1, want 0
- `Coronal slice filter` [nonzero_exit]: exit code 1, want 0
- `Axial slice filter` [nonzero_exit]: exit code 1, want 0
- `Slab filter (multiple slices)` [nonzero_exit]: exit code 1, want 0
- `Sagittal slice exclusion` [nonzero_exit]: exit code 1, want 0
- `End point sagittal filter` [nonzero_exit]: exit code 1, want 0
- `U-factor threshold` [nonzero_exit]: exit code 1, want 0
- `Curvature threshold` [nonzero_exit]: exit code 1, want 0
- `Torsion threshold` [nonzero_exit]: exit code 1, want 0
- `Skip tracks` [nonzero_exit]: exit code 1, want 0
- `Proportional skip` [nonzero_exit]: exit code 1, want 0
- `Extend tracks` [nonzero_exit]: exit code 1, want 0
- `ROI pointer (single voxel)` [nonzero_exit]: exit code 1, want 0
- `Dual ROI pointers` [nonzero_exit]: exit code 1, want 0
- `ROI disk filter` [nonzero_exit]: exit code 1, want 0
- `Output filtered tracks` [nonzero_exit]: exit code 1, want 0
- `Output track volume` [nonzero_exit]: exit code 1, want 0
- `Output endpoint volume` [nonzero_exit]: exit code 1, want 0
- `Save camera position` [nonzero_exit]: exit code 1, want 0
- `Camera azimuth rotation` [nonzero_exit]: exit code 1, want 0
- `Camera elevation rotation` [nonzero_exit]: exit code 1, want 0
- `Camera dolly (zoom)` [nonzero_exit]: exit code 1, want 0
- `Camera offset` [nonzero_exit]: exit code 1, want 0
- `Solid color option` [nonzero_exit]: exit code 1, want 0
- `Directional color coding` [nonzero_exit]: exit code 1, want 0
- `Transparent display` [nonzero_exit]: exit code 1, want 0
- `Shading option` [nonzero_exit]: exit code 1, want 0
- `Wireframe option` [nonzero_exit]: exit code 1, want 0
- `Tube radius` [nonzero_exit]: exit code 1, want 0
- `Number of sides` [nonzero_exit]: exit code 1, want 0
- `Frame box display` [nonzero_exit]: exit code 1, want 0
- `Background color` [nonzero_exit]: exit code 1, want 0
- `Window size` [nonzero_exit]: exit code 1, want 0
- `Anti-aliasing` [nonzero_exit]: exit code 1, want 0
- `Title overlay` [nonzero_exit]: exit code 1, want 0
- `No annotation` [nonzero_exit]: exit code 1, want 0
- `Ball marker` [nonzero_exit]: exit code 1, want 0
- `Circle display` [nonzero_exit]: exit code 1, want 0
- `Alternative color coding` [nonzero_exit]: exit code 1, want 0
- `Helix color coding` [nonzero_exit]: exit code 1, want 0
- `Axis thickness` [nonzero_exit]: exit code 1, want 0
- `Brain image overlay` [nonzero_exit]: exit code 1, want 0
- `Axial brain slice` [nonzero_exit]: exit code 1, want 0
- `Sagittal brain slice` [nonzero_exit]: exit code 1, want 0
- `Coronal brain slice` [nonzero_exit]: exit code 1, want 0
- `Window level adjustment` [nonzero_exit]: exit code 1, want 0
- `ROI from NIfTI file` [nonzero_exit]: exit code 1, want 0
- `Dual ROI files` [nonzero_exit]: exit code 1, want 0
- `Output ROI volume` [nonzero_exit]: exit code 1, want 0
- `ROI display with tracks` [nonzero_exit]: exit code 1, want 0
- `Background tracks` [nonzero_exit]: exit code 1, want 0
- `End ROI filter` [nonzero_exit]: exit code 1, want 0
- `Both ends ROI filter` [nonzero_exit]: exit code 1, want 0
- `End point iteration` [nonzero_exit]: exit code 1, want 0
- `ROI tube filter` [nonzero_exit]: exit code 1, want 0
- `Screen capture magnification` [nonzero_exit]: exit code 1, want 0
- `Custom log ID` [nonzero_exit]: exit code 1, want 0
- `Disable log` [nonzero_exit]: exit code 1, want 0
- `Duplicate arguments` [nonzero_exit]: exit code 1, want 0
- `Complex filter pipeline` [nonzero_exit]: exit code 1, want 0
- `Filter with brain overlay` [nonzero_exit]: exit code 1, want 0
- `Filter with ROI and output` [nonzero_exit]: exit code 1, want 0
- `Full display pipeline` [nonzero_exit]: exit code 1, want 0
- `Surface file generation` [nonzero_exit]: exit code 1, want 0
- `Surface threshold` [nonzero_exit]: exit code 1, want 0
- `Lattice ROI filter` [nonzero_exit]: exit code 1, want 0
- `KT threshold` [nonzero_exit]: exit code 1, want 0

## tractseg

- Passed: 115
- Failed: 4
- Skipped: 0

- `maskfilter dilate` [command_not_found]: exit code 127, want 0
- `maskfilter erode` [command_not_found]: exit code 127, want 0
- `maskfilter clean` [command_not_found]: exit code 127, want 0
- `maskfilter median` [command_not_found]: exit code 127, want 0

## vesselapp

- Passed: 73
- Failed: 1
- Skipped: 0

- `nibabel get_fdata` [missing_output]: missing output fragment 'ndim: 3'
