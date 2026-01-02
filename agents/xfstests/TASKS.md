## xfstests error summary + task list

Source log: `agents/xfstests/xfs_output.log`  
xfstests tree: `local/xfstests/`

### Run context (from log header)

- **FSTYP**: virtiofs
- **PLATFORM**: Linux/x86_64 tinyrange 6.18.2-0-virt #1-Alpine SMP PREEMPT_DYNAMIC 2025-12-29 10:24:58
- **MKFS_OPTIONS**: scratchfs
- **MOUNT_OPTIONS**: scratchfs /scratch

### Outcome summary

From the end-of-run footer in `xfs_output.log`:

- **Invoked**: 629 tests
- **Failures**: 57
- **Not run (skipped)**: 486
- **Passed**: 86 (computed as \(629 - 57 - 486\))
- **Exit status**: 1

### High-noise “environment” errors (fix these first)

- **`tac` temporary file failures (143 log lines)**: repeated `tac: /tmp/cutmpXXXX: write error: No such file or directory`
  - **Why this matters**: xfstests uses `tac` when slicing logs (notably in `local/xfstests/common/rc` via pipelines like `dmesg | tac | … | tac`). When `tac` reads from a pipe, some implementations spill to a temp file; if `/tmp` isn’t usable, you get spam and occasionally output mismatches (e.g. `generic/247`).
  - **Likely fix**: ensure the guest/container running xfstests has a writable `/tmp` (tmpfs is fine) and/or a valid `TMPDIR`.
- **Missing `/dev/fd/*` (seen in `generic/260` and `generic/288`)**: `grep: /dev/fd/63: No such file or directory`
  - **Likely fix**: ensure `procfs` and the usual `/dev/fd` symlink are mounted/created inside the guest/container.

### Failure clusters (root-cause buckets)

- **Directory iteration / readdir/getdents correctness**
  - **Failures**: `generic/006`, `generic/257`, `generic/471`, `generic/637`, `generic/676`, `generic/736`
  - **Signals**:
    - Large dirs appear truncated (`generic/006` created count 1023 vs 4097; `generic/736` expects 5002 entries but sees 1024).
    - `getdents` returns 0 early / inconsistent inodes (`generic/637`).
    - POSIX `rewinddir(3)` semantics violated (`generic/471`).
    - Cleanup fails with “Directory not empty” (`generic/257`, `generic/676`, `generic/736`).
  - **xfstests sources**: `local/xfstests/src/t_dir_offset2.c`, `local/xfstests/src/rewinddir-test.c`, `local/xfstests/tests/generic/257`, `local/xfstests/tests/generic/471`, `local/xfstests/tests/generic/637`.

- **Extended attributes (xattrs) and xattr-driven features**
  - **Failures** (xattr syscall failures / missing output): `generic/020`, `generic/062`, `generic/097`, `generic/337`, `generic/377`, `generic/403`, `generic/486`, `generic/523`, `generic/529`, `generic/533`, `generic/618`
  - **Signals**: `getfattr` / `setfattr` / `listxattr` returning **Operation not supported**, and expected attribute dumps missing.
  - **xfstests sources**: `local/xfstests/common/attr`, `local/xfstests/tests/generic/020`, `.../062`, `.../097`, `.../337`, `.../377`, `.../403`, `.../486`, `.../523`, `.../529`, `.../533`, `.../618`.

- **ACLs / setgid inheritance / protected_* sysctls (permissions model)**
  - **Failures**:
    - ACL-related: `generic/099`, `generic/307`, `generic/319`, `generic/375`
    - SGID inheritance / setgid behavior: `generic/314`, `generic/444`, `generic/633`, `generic/696`, `generic/697`
    - Sysctl protections: `generic/597`, `generic/598`
  - **Signals**:
    - ACL changes not reflected (`generic/099`), default ACL inheritance missing (`generic/319`).
    - Setgid bit not inherited (`generic/314`, `generic/444`) and vfstest failures around `is_setgid` (`generic/633`, `generic/696`, `generic/697`).
    - protected_symlinks / protected_regular not enforced (`generic/597`, `generic/598`).
  - **xfstests sources**: `local/xfstests/common/attr`, `local/xfstests/src/vfs/vfstest*`, `local/xfstests/tests/generic/597`, `.../598`.

- **ctime / timestamp correctness**
  - **Failures**: `generic/221`, `generic/236`, `generic/258`, `generic/307`, `generic/423`, `generic/728`, `generic/755`
  - **Signals**:
    - ctime not updated on metadata changes (link/unlink/setfacl/setxattr/futimens).
    - nanosecond ordering failure in `statx` (`generic/423`).
    - pre-epoch timestamp handling wraps (`generic/258`).

- **Preallocation / fallocate support**
  - **Failures**: `generic/075`, `generic/112`, `generic/263`, `generic/759`, `generic/760`
  - **Signals**:
    - fsx prealloc variants skipped (075/112), and fsx prints “filesystem does not support fallocate … disabling” (263/759/760).
  - **Note**: this also explains many `[not run] xfs_io falloc* failed` skips.

- **Data integrity / coherency (mmap, O_DIRECT, copy_file_range)**
  - **Failures**: `generic/029`, `generic/030`, `generic/091`, `generic/130`, `generic/432`, `generic/433`, `generic/451`, `generic/759`, `generic/760`
  - **Signals**:
    - Mapped write + truncate/remount loses data (`generic/029`, `generic/030`).
    - direct/buffered coherency issues (`generic/130`, `generic/451`).
    - `copy_file_range` produces differing copies (`generic/432`, `generic/433`).

- **File locks / leases**
  - **Failures**: `generic/131` (locktest), `generic/571` (leasetest)
  - **xfstests sources**: `local/xfstests/common/locktest`, `local/xfstests/src/locktest.c`.

- **SEEK_HOLE / SEEK_DATA**
  - **Failures**: `generic/448`, `generic/490`

- **Special file types**
  - **Failures**: `generic/401` (mknod-created block/char nodes show up as regular files)

- **Discard / FITRIM**
  - **Failures**: `generic/260`, `generic/288`
  - **Signals**:
    - `FITRIM` reports “discard operation is not supported” where the test expected argument-validation errors.
    - Also affected by missing `/dev/fd/*`.

### Full failure list (57) — per-test summary

Each row ties the failure back to the corresponding xfstests script under `local/xfstests/tests/...`.

| Test | What the test checks (from `local/xfstests/tests/...`) | Observed failure (from `agents/xfstests/xfs_output.log`) |
|---|---|---|
| `generic/006` | permname | 1023 files created |
| `generic/020` | extended attributes | getfattr: <TESTFILE>: Operation not supported |
| `generic/029` | Test mapped writes against truncate down/up to ensure we get the data | 000800 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00  >................< |
| `generic/030` | Test mapped writes against remap+truncate down/up to ensure we get the data | 4e6400 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00  >................< |
| `generic/035` | Check overwriting rename system call | nlink is 1, should be 0 |
| `generic/062` | Exercises the getfattr/setfattr tools | getfattr: SCRATCH_MNT/reg: Operation not supported |
| `generic/075` | fsx (non-AIO variant) | fsx prealloc variants skipped (xfs_io resvsp/fallocate unsupported) |
| `generic/091` | fsx exercising direct IO -- sub-block sizes and concurrent buffered IO | output expanded to ~230 lines (see .full/.out.bad for details) |
| `generic/097` | simple attr tests for EAs: | missing expected xattr output: trusted.colour="marone" |
| `generic/099` | Test out ACLs. | file1 [u::rwx,g::rw-,o::r--] |
| `generic/112` | fsx (AIO variant, based on 075) | fsx prealloc variants skipped (xfs_io resvsp/fallocate unsupported) |
| `generic/130` | xfs_io vector read/write and trunc tests. | 00001380:  57 57 57 57 57 57 57 57 79 79 79 79 79 79 79 79  WWWWWWWWyyyyyyyy |
| `generic/131` | lock test created from CXFSQA test lockfile_simple | Client reported failure (1) |
| `generic/193` | Test permission checks in ->setattr | suid/sgid bits not behaving as expected (observed `-rw-rw-rw-` where suid was expected) |
| `generic/221` | Check ctime updates when calling futimens without UTIME_OMIT for the | failed to update ctime! |
| `generic/236` | Check ctime updated or not if file linked | Fatal error: ctime not updated after link |
| `generic/247` | Test for race between direct I/O and mmap | tac: /tmp/cutmpGqUXFO: write error: No such file or directory |
| `generic/257` | Check that no duplicate d_off values are returned and that those | rm: cannot remove '/mnt/ttt': Directory not empty |
| `generic/258` | Test timestamps prior to epoch | Timestamp wrapped: 1767338820 |
| `generic/260` | Purpose of this test is to check FITRIM argument handling to make sure | grep: /dev/fd/63: No such file or directory |
| `generic/263` | fsx exercising direct IO vs sub-block buffered I/O | main: filesystem does not support fallocate mode 0, disabling! |
| `generic/288` | This check the FITRIM argument handling in the corner case where length is | grep: /dev/fd/63: No such file or directory |
| `generic/307` | Check if ctime is updated and written to disk after setfacl | error: ctime not updated after setfacl |
| `generic/314` | Test SGID inheritance on subdirectories | drwxr-xr-x subdir |
| `generic/319` | Regression test to make sure a directory inherits the default ACL from | default ACL entries missing from getfacl output |
| `generic/337` | Test that the filesystem's implementation of the listxattrs system call lists | missing expected xattr output: user.J3__T_Km3dVsW_="hello" |
| `generic/355` | Test clear of suid/sgid on direct write. | before: -rw-r--r-- |
| `generic/375` | Check if SGID is cleared upon chmod / setfacl when the owner is not in the | -rwxr-xr-x |
| `generic/377` | Test listxattr syscall behaviour with different buffer sizes. | listxattr: Operation not supported |
| `generic/401` | Test filetype feature | device node types reported as regular files (`b f`, `c f`) |
| `generic/403` | Test racing getxattr requests against large xattr add and remove loop. | setfattr: /scratch/file: Operation not supported |
| `generic/423` | Test the statx system call | stat_test failed |
| `generic/432` | Tests vfs_copy_file_range(): | /mnt/test-432/file /mnt/test-432/copy differ: char 1, line 1 |
| `generic/433` | Tests vfs_copy_file_range(): | /mnt/test-433/copy - differ: char 1, line 1 |
| `generic/444` | Check if SGID is inherited when creating a subdirectory when the owner is not | drwxr-xr-x |
| `generic/448` | Check what happens when SEEK_HOLE/SEEK_DATA are fed negative offsets. | seek sanity check failed! |
| `generic/451` | Test data integrity when mixing buffered reads and asynchronous | get stale data from buffer read |
| `generic/471` | Test that if names are added to a directory after an opendir(3) call and | File name 2 appeared 0 times |
| `generic/486` | Ensure that we can XATTR_REPLACE a tiny attr into a large attr. | attr_list: Operation not supported |
| `generic/490` | Check that SEEK_DATA works properly for offsets in the middle of large holes. | seek sanity check failed! |
| `generic/523` | Check that xattrs can have slashes in their name. | getfattr: /scratch/moofile: Operation not supported |
| `generic/529` | Regression test for a bug where XFS corrupts memory if the listxattr buffer | list attr: Operation not supported |
| `generic/533` | Simple attr smoke tests for user EAs, dereived from generic/097. | getfattr: TEST_DIR/foo.533: Operation not supported |
| `generic/571` | lease test | Client reported failure (1) |
| `generic/597` | Test protected_symlink and protected_hardlink sysctls | successfully followed symlink |
| `generic/598` | Test protected_regular and protected_fifos sysctls | expected Permission denied, but operation succeeded |
| `generic/618` | Verify that forkoff can be returned as 0 properly if it isn't | getfattr: /scratch/testfile: Operation not supported |
| `generic/633` | Test that idmapped mounts behave correctly. | vfstest.c: 1532: setgid_create - Success - failure: is_setgid |
| `generic/637` | Check that directory modifications to an open dir are observed | getdents returned 0 on entry 7 |
| `generic/676` | Test that filesystem properly handles seeking in directory both to valid | rm: cannot remove '/mnt/676-dir': Directory not empty |
| `generic/696` | Test S_ISGID stripping whether works correctly when call process | vfstest.c: 1781: setgid_create_umask - Success - failure: is_setgid |
| `generic/697` | Test S_ISGID stripping whether works correctly when call process | vfstest.c: 1960: setgid_create_acl - Success - failure: is_setgid |
| `generic/728` | Test a bug where the NFS client wasn't sending a post-op GETATTR to the | Expected ctime to change after setxattr. |
| `generic/736` | Test that on a fairly large directory if we keep renaming files while holding | rm: cannot remove '/mnt/test-736/testdir': Directory not empty |
| `generic/755` | Create a file, stat it and then unlink it. | Target's ctime did not change after unlink! |
| `generic/759` | fsx exercising reads/writes from userspace buffers | main: filesystem does not support fallocate mode 0, disabling! |
| `generic/760` | fsx exercising direct IO reads/writes from userspace buffers | mapped writes DISABLED |

### Skipped/not-run tests (486) — top reasons

There are **72** unique `[not run]` reasons in the log; the top ones are:

- **106×** Reflink not supported by scratch filesystem type: virtiofs
- **70×** require scratchfs to be valid block disk
- **35×** Reflink not supported by test filesystem type: virtiofs
- **27×** xfs_io falloc failed (old kernel/wrong fs?)
- **23×** disk quotas not supported by this filesystem type: virtiofs
- **23×** No encryption support for virtiofs
- **19×** xfs_io fpunch failed (old kernel/wrong fs?)
- **15×** virtiofs does not support shutdown
- **15×** xfs_io exchangerange support is missing
- **12×** swapfiles are not supported
- **9×** fsverity utility required, skipped this test
- **8×** xfs_io falloc -k failed (old kernel/wrong fs?)
- **8×** Dedupe not supported by test filesystem type: virtiofs
- **7×** xfs_io fzero failed (old kernel/wrong fs?)
- **6×** Dedupe not supported by scratch filesystem type: virtiofs

### Task list

#### Fix the test environment noise (unblocks cleaner signal)

- [ ] Ensure the xfstests guest/container has a writable `/tmp` (or `TMPDIR`) so `tac` can create temp files.
  - [ ] Verify `dmesg | tac | head` produces no `tac: /tmp/cutmp...` errors.
  - [ ] Re-run `generic/247` to confirm it no longer fails due to `tac` output.
- [ ] Ensure `/dev/fd` exists (procfs + symlinks), so process substitution / FD paths work.
  - [ ] Re-run `generic/260` and `generic/288` to confirm the `grep: /dev/fd/..` errors are gone.

#### Directory semantics (readdir/getdents/seekdir/rewinddir)

- [ ] Fix directory enumeration pagination/cookies so large directories don’t truncate around ~1024 entries.
  - Affects: `generic/006`, `generic/736`
- [ ] Fix `getdents64` / dir offset (`d_off`) correctness: no premature 0 returns, stable offsets, and correct inode/name pairs.
  - Affects: `generic/257`, `generic/637`
- [ ] Ensure `rewinddir(3)` observes names added after the initial `opendir(3)` call (POSIX requirement).
  - Affects: `generic/471`
- [ ] Fix directory mutation cleanup behavior so `rm -rf` / `rmdir` doesn’t hit “Directory not empty” spuriously.
  - Affects: `generic/257`, `generic/676`, `generic/736`

#### xattrs + ACLs + permission model

- [ ] Implement/enable xattr syscalls end-to-end: `setxattr/getxattr/listxattr/removexattr` (at least `user.*` and `trusted.*` used by tests).
  - Affects: `generic/020`, `generic/062`, `generic/097`, `generic/337`, `generic/377`, `generic/403`, `generic/486`, `generic/523`, `generic/529`, `generic/533`, `generic/618`
- [ ] Ensure xattr changes update ctime (and are visible after remount where tests require persistence).
  - Affects: `generic/728` (and indirectly several xattr tests)
- [ ] Make ACL operations round-trip correctly (set via `chacl`/`setfacl`, read back matches expectations).
  - Affects: `generic/099`, `generic/307`, `generic/319`, `generic/375`
- [ ] Fix SGID inheritance / stripping rules (including under umask/ACL paths) so directories/files get correct SGID behavior.
  - Affects: `generic/314`, `generic/444`, `generic/633`, `generic/696`, `generic/697`
- [ ] Ensure `fs.protected_*` sysctls are enforced (or document why they can’t be for this setup).
  - Affects: `generic/597`, `generic/598`

#### Timestamps (ctime / statx / pre-epoch)

- [ ] Ensure ctime updates on metadata operations: `link`, `unlink`, `setfacl`, `setxattr`, `futimens`, etc.
  - Affects: `generic/221`, `generic/236`, `generic/307`, `generic/728`, `generic/755`
- [ ] Fix `statx` timestamp ordering / nsec handling (avoid `ctime.nsec` going “backwards”).
  - Affects: `generic/423`
- [ ] Fix pre-epoch timestamp handling (negative seconds since epoch must not wrap).
  - Affects: `generic/258`

#### Fallocate / preallocation / hole-data support

- [ ] Implement/enable fallocate variants needed by xfs_io/fsx (`resvsp`, `falloc`, `FALLOC_FL_KEEP_SIZE`, etc.).
  - Affects: `generic/075`, `generic/112`, `generic/263`, `generic/759`, `generic/760`
- [ ] Implement correct SEEK_HOLE/SEEK_DATA behavior (including validation of negative offsets).
  - Affects: `generic/448`, `generic/490`

#### Data integrity / coherency

- [ ] Fix mmap write + truncate + remount persistence (no zeros where data was written).
  - Affects: `generic/029`, `generic/030`
- [ ] Fix buffered vs direct I/O coherency (no stale reads / wrong content).
  - Affects: `generic/130`, `generic/451`, and likely contributes to `generic/091`
- [ ] Fix `copy_file_range` integrity (copied file must match source across operations).
  - Affects: `generic/432`, `generic/433`

#### Locking / leasing

- [ ] Implement/enable POSIX advisory locks (fcntl) semantics required by locktest.
  - Affects: `generic/131`
- [ ] Implement/enable `fcntl(F_SETLEASE)` behavior (or mark unsupported explicitly).
  - Affects: `generic/571`

#### Other correctness gaps

- [ ] Fix rename-overwrite link count visibility on open FDs (overwritten inode must report `st_nlink == 0`).
  - Affects: `generic/035` (see `local/xfstests/src/t_rename_overwrite.c`)
- [ ] Support special file types created by `mknod` (char/block) so directory entry types are correct.
  - Affects: `generic/401`
- [ ] Decide whether to support FITRIM/discard; if unsupported, ensure error semantics match tests’ expectations or skip.
  - Affects: `generic/260`, `generic/288`


