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
- **Failures**: 48
- **Not run (skipped)**: 486
- **Passed**: 95 (computed as \(629 - 48 - 486\))
- **Exit status**: 1

### High-noise “environment” errors (fix these first)

- **Interleaved `cc` logging in test output (1 line)**: `time=... level=INFO msg="virtiofs lseek" ...` shows up in the main xfstests stream (right before `generic/075`).
  - **Why this matters**: xfstests diffs compare stdout; stray log lines can turn real kernel/FS bugs into “output mismatch” noise.
  - **Likely fix**: ensure `cc`/virtiofs logs go to stderr or a file (or are disabled) during xfstests runs.

### Failure clusters (root-cause buckets)

- **Extended attributes (xattrs) and xattr-driven features**
  - **Failures** (xattr syscall failures / missing output): `generic/020`, `generic/062`, `generic/097`, `generic/337`, `generic/377`, `generic/403`, `generic/486`, `generic/523`, `generic/529`, `generic/533`, `generic/618`, `generic/728`
  - **Signals**: `getfattr` / `setfattr` / `listxattr` returning **Operation not supported**, and expected attribute dumps missing.
  - **xfstests sources**: `local/xfstests/common/attr`, `local/xfstests/tests/generic/020`, `.../062`, `.../097`, `.../337`, `.../377`, `.../403`, `.../486`, `.../523`, `.../529`, `.../533`, `.../618`, `.../728`.

- **ACLs / setgid inheritance / protected_* sysctls (permissions model)**
  - **Failures**:
    - ACL-related: `generic/099`, `generic/307`, `generic/319`, `generic/375`
    - SGID inheritance / setgid behavior: `generic/314`, `generic/444`, `generic/633`, `generic/696`, `generic/697`
    - suid/sgid bit semantics: `generic/193`, `generic/355`
    - Sysctl protections: `generic/597`, `generic/598`
  - **Signals**:
    - ACL changes not reflected (`generic/099`), default ACL inheritance missing (`generic/319`).
    - Setgid bit not inherited (`generic/314`, `generic/444`) and vfstest failures around `is_setgid` (`generic/633`, `generic/696`, `generic/697`).
    - suid/sgid bits not preserved / not cleared as expected under chmod/chown/direct I/O (`generic/193`, `generic/355`).
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

### Full failure list (48) — per-test summary

Each row ties the failure back to the corresponding xfstests script under `local/xfstests/tests/...`.

| Test | What the test checks (from `local/xfstests/tests/...`) | Observed failure (from `agents/xfstests/xfs_output.log`) |
|---|---|---|
| `generic/020` | extended attributes | getfattr: <TESTFILE>: Operation not supported |
| `generic/029` | Test mapped writes against truncate down/up to ensure we get the data correctly written. | 000800 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00  >................< |
| `generic/030` | Test mapped writes against remap+truncate down/up to ensure we get the data correctly written. | 4e6400 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00  >................< |
| `generic/035` | Check overwriting rename system call | nlink is 1, should be 0 |
| `generic/062` | Exercises the getfattr/setfattr tools | getfattr: SCRATCH_MNT/reg: Operation not supported |
| `generic/075` | fsx (non-AIO variant) | fsx prealloc variants skipped (xfs_io resvsp/fallocate unsupported) |
| `generic/091` | fsx exercising direct IO -- sub-block sizes and concurrent buffered IO | output expanded to ~230 lines (see .full/.out.bad for details) |
| `generic/097` | simple attr tests for EAs: | missing expected xattr output: trusted.colour="marone" |
| `generic/099` | Test out ACLs. | file1 [u::rwx,g::rw-,o::r--] |
| `generic/112` | fsx (AIO variant, based on 075) | fsx prealloc variants skipped (xfs_io resvsp/fallocate unsupported) |
| `generic/130` | xfs_io vector read/write and trunc tests. modified from cxfsqa tests | 00001380:  57 57 57 57 57 57 57 57 79 79 79 79 79 79 79 79  WWWWWWWWyyyyyyyy |
| `generic/131` | lock test created from CXFSQA test lockfile_simple | Client reported failure (1) |
| `generic/193` | Test permission checks in ->setattr | suid/sgid bits not behaving as expected (observed `-rw-rw-rw-` where suid was expected) |
| `generic/221` | Check ctime updates when calling futimens without UTIME_OMIT for the mtime entry. | failed to update ctime! |
| `generic/236` | Check ctime updated or not if file linked | Fatal error: ctime not updated after link |
| `generic/258` | Test timestamps prior to epoch | Timestamp wrapped: 1767430359 |
| `generic/263` | fsx exercising direct IO vs sub-block buffered I/O | main: filesystem does not support fallocate mode 0, disabling! |
| `generic/307` | Check if ctime is updated and written to disk after setfacl | error: ctime not updated after setfacl |
| `generic/314` | Test SGID inheritance on subdirectories | drwxr-xr-x subdir |
| `generic/319` | Regression test to make sure a directory inherits the default ACL from its parent directory. | default ACL entries missing from getfacl output |
| `generic/337` | Test that the filesystem's implementation of the listxattrs system call lists all the xattrs an inode has. | missing expected xattr output: user.J3__T_Km3dVsW_="hello" |
| `generic/355` | Test clear of suid/sgid on direct write. | before: -rw-r--r-- |
| `generic/375` | Check if SGID is cleared upon chmod / setfacl when the owner is not in the owning group. | -rwxr-xr-x |
| `generic/377` | Test listxattr syscall behaviour with different buffer sizes. | listxattr: Operation not supported |
| `generic/401` | Test filetype feature | device node types reported as regular files (`b f`, `c f`) |
| `generic/403` | Test racing getxattr requests against large xattr add and remove loop. | setfattr: /scratch/file: Operation not supported |
| `generic/423` | Test the statx system call | stat_test failed |
| `generic/432` | Tests vfs_copy_file_range(): | /mnt/test-432/file /mnt/test-432/copy differ: char 1, line 1 |
| `generic/433` | Tests vfs_copy_file_range(): | /mnt/test-433/copy - differ: char 1, line 1 |
| `generic/444` | Check if SGID is inherited when creating a subdirectory when the owner is not in the owning group and directory has default ACLs. | drwxr-xr-x |
| `generic/448` | Check what happens when SEEK_HOLE/SEEK_DATA are fed negative offsets. | seek sanity check failed! |
| `generic/451` | Test data integrity when mixing buffered reads and asynchronous direct writes a file. | get stale data from buffer read |
| `generic/486` | Ensure that we can XATTR_REPLACE a tiny attr into a large attr. | attr_list: Operation not supported |
| `generic/490` | Check that SEEK_DATA works properly for offsets in the middle of large holes. | seek sanity check failed! |
| `generic/523` | Check that xattrs can have slashes in their name. | getfattr: /scratch/moofile: Operation not supported |
| `generic/529` | Regression test for a bug where XFS corrupts memory if the listxattr buffer | list attr: Operation not supported |
| `generic/533` | Simple attr smoke tests for user EAs, dereived from generic/097. | getfattr: TEST_DIR/foo.533: Operation not supported |
| `generic/571` | lease test | Client reported failure (1) |
| `generic/597` | Test protected_symlink and protected_hardlink sysctls | successfully followed symlink |
| `generic/598` | Test protected_regular and protected_fifos sysctls | expected Permission denied, but operation succeeded |
| `generic/618` | Verify that forkoff can be returned as 0 properly if it isn't able to fit inline for XFS. | getfattr: /scratch/testfile: Operation not supported |
| `generic/633` | Test that idmapped mounts behave correctly. | vfstest.c: 1532: setgid_create - Success - failure: is_setgid |
| `generic/696` | Test S_ISGID stripping whether works correctly when call process | vfstest.c: 1781: setgid_create_umask - Success - failure: is_setgid |
| `generic/697` | Test S_ISGID stripping whether works correctly when call process | vfstest.c: 1960: setgid_create_acl - Success - failure: is_setgid |
| `generic/728` | Test a bug where the NFS client wasn't sending a post-op GETATTR to the server after setting an xattr, resulting in `stat` reporting a stale ctime. | Expected ctime to change after setxattr. |
| `generic/755` | Create a file, stat it and then unlink it. | Target's ctime did not change after unlink! |
| `generic/759` | fsx exercising reads/writes from userspace buffers backed by hugepages | main: filesystem does not support fallocate mode 0, disabling! |
| `generic/760` | fsx exercising direct IO reads/writes from userspace buffers backed by hugepages | mapped writes DISABLED |

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

- [ ] Ensure `cc`/virtiofs logs do not interleave with xfstests stdout (keep the harness output clean).
  - [ ] Re-run `generic/075` and confirm there are no `level=INFO msg="virtiofs ..."` lines in the output stream.

#### xattrs + ACLs + permission model

- [ ] Implement/enable xattr syscalls end-to-end: `setxattr/getxattr/listxattr/removexattr` (at least `user.*` and `trusted.*` used by tests).
  - Affects: `generic/020`, `generic/062`, `generic/097`, `generic/337`, `generic/377`, `generic/403`, `generic/486`, `generic/523`, `generic/529`, `generic/533`, `generic/618`, `generic/728`
- [ ] Ensure xattr changes update ctime (and are visible after remount where tests require persistence).
  - Affects: `generic/728` (and indirectly several xattr tests)
- [ ] Make ACL operations round-trip correctly (set via `chacl`/`setfacl`, read back matches expectations).
  - Affects: `generic/099`, `generic/307`, `generic/319`, `generic/375`, `generic/444`
- [ ] Fix SGID inheritance / stripping rules (including under umask/ACL paths) so directories/files get correct SGID behavior.
  - Affects: `generic/314`, `generic/444`, `generic/633`, `generic/696`, `generic/697`
- [ ] Fix suid/sgid bit semantics (preserve/clear on chmod/chown/direct I/O per Linux VFS rules).
  - Affects: `generic/193`, `generic/355`
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


