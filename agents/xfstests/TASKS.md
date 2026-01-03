## xfstests error summary + task list

Source log: `agents/xfstests/xfs_output.log`  
xfstests tree: `local/xfstests/`

### Run context (from log header)

- **FSTYP**: virtiofs
- **PLATFORM**: Linux/x86_64 tinyrange 6.18.2-0-virt #1-Alpine SMP PREEMPT_DYNAMIC 2025-12-29 10:24:58
- **MKFS_OPTIONS**: scratchfs
- **MOUNT_OPTIONS**: scratchfs /scratch

### Outcome summary

From the per-test lines + footer in `xfs_output.log`:

- **Invoked**: 629 tests
- **Failures**: 5 (`generic/001`, `generic/023`, `generic/131`, `generic/478`, `generic/571`)
- **Not run (skipped)**: 465
- **Passed**: 159 (computed as \(629 - 5 - 465\))
- **Exit status**: 1

### Failure clusters (root-cause buckets)

- **Create/write/unlink + potential corruption**: `generic/001`
- **renameat2(2) compatibility (flags=0)**: `generic/023`
- **Locking / leases (fcntl)**: `generic/131`, `generic/478` (OFD locks), `generic/571` (leases)

### Full failure list (5) — per-test summary

Each row ties the failure back to the corresponding xfstests script under `local/xfstests/tests/...`.

| Test | What the test checks (from `local/xfstests/tests/...`) | Observed failure (from `agents/xfstests/xfs_output.log`) |
|---|---|---|
| `generic/001` | Exercises `creat`/`write`/`unlink` across many dir sizes; checks for corruption by chaining copies. | Output mismatch; expected to complete 5 iterations, but actual output diverged after `iter 1` (see `generic/001.out.bad`). |
| `generic/023` | `renameat2()` syscall without flags (rename compatibility), matrix over src/dst types. | `samedir regu/regu` and `samedir regu/symb` returned **Directory not empty**; expected success (`none/regu.`). |
| `generic/131` | POSIX advisory locking smoke test (`common/locktest`). | `Client reported failure (1)` (see `generic/131.full`). |
| `generic/478` | OFD lock semantics (`F_OFD_SETLK` + `F_OFD_GETLK`) across clone/dup/close scenarios. | Output mismatch in lock advice stream (see `generic/478.out.bad` / `.full`). |
| `generic/571` | `fcntl(F_SETLEASE)` lease semantics (`common/locktest` lease runner). | `Client reported failure (1)` (see `generic/571.full`). |

### Skipped/not-run tests (465) — top reasons

There are **72** unique `[not run]` reasons in the log; the most common are:

- **107×** Reflink not supported by scratch filesystem type: virtiofs
- **72×** require scratchfs to be valid block disk
- **35×** Reflink not supported by test filesystem type: virtiofs
- **23×** disk quotas not supported by this filesystem type: virtiofs
- **23×** No encryption support for virtiofs
- **19×** xfs_io fiemap  failed (old kernel/wrong fs?)
- **15×** virtiofs does not support shutdown
- **15×** xfs_io exchangerange  support is missing
- **13×** swapfiles are not supported
- **9×** xfs_io fzero  failed (old kernel/wrong fs?)
- **9×** fsverity utility required, skipped this test
- **8×** Filesystem virtiofs not supported in _scratch_mkfs_sized
- **8×** Dedupe not supported by test filesystem type: virtiofs
- **6×** Dedupe not supported by scratch filesystem type: virtiofs
- **5×** kernel doesn't support renameat2 syscall
- **5×** virtiofs doesn't support open_by_handle_at(2)

### Task list

#### P0 — capture complete failure artifacts (unblocks real debugging)

- [ ] For each failing test (`001`, `023`, `131`, `478`, `571`), capture:
  - [ ] `.../generic/<test>.out.bad` (full diff, not truncated)
  - [ ] `.../generic/<test>.full` (the test’s detailed output)
  - [ ] Any `seqres.full` snippets around the failure if present
  - Note: paths are shown in `agents/xfstests/xfs_output.log` under `/opt/xfstests/results//generic/...`.

#### P1 — fix locking + leases (3/5 failures)

- [ ] **POSIX advisory locks**: make `fcntl(F_SETLK/F_SETLKW/F_GETLK)` semantics pass `generic/131` (`common/locktest`).
- [ ] **OFD locks**: make `F_OFD_SETLK/F_OFD_GETLK` semantics match Linux expectations in `generic/478` (clone/dup/close behavior).
- [ ] **Leases**: either implement correct `fcntl(F_SETLEASE)` behavior or return a consistent “unsupported” error so `_require_test_fcntl_setlease` skips `generic/571` instead of failing.

#### P1 — fix `renameat2(2)` flags=0 compatibility (1/5 failures)

- [ ] Make `renameat2(old, new, 0)` behave like `renameat` for regular files/symlinks: `generic/023` should succeed for `samedir regu/regu` and `samedir regu/symb` (no spurious `ENOTEMPTY`).

#### P1 — investigate `generic/001` early abort/corruption (1/5 failures)

- [ ] Inspect `generic/001.out.bad`/`.full` to determine if the failure is:
  - [ ] **vanishing files** (`Error: <file> vanished!`)
  - [ ] **data corruption** (`Error: corruption for <file> ...`)
  - [ ] **unexpected mkdir/create failures** in `_setup()`
- [ ] Once the symptom is known, add a minimal reproduction (single-test run) + capture a `cc` debug log (see `run_xfstest.sh`) around the failing ops.

#### P2 — reduce skip rate (optional / coverage work)

- [ ] Provide a block-backed scratch device (or enable loopback) so tests that require “scratchfs to be a valid block disk” can run.
- [ ] Decide which virtiofs features are in-scope (reflink/dedupe/encryption/quota/shutdown) vs expected skips, and document that policy.

