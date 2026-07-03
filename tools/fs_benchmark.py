#!/usr/bin/env python3
"""Benchmark host filesystem performance against CrumbleCracker shared I/O.

The benchmark runs the same Python workload twice:

1. directly on the host
2. inside a CrumbleCracker VM against a writable shared host directory

It is intentionally focused on patterns that show up in the fulltest data:
many small files, metadata-heavy loops, sequential large-file I/O, and random
4 KiB I/O inside a large file.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import json
import os
import platform
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[1]
PYNEURODESK_SRC = REPO_ROOT / "pyneurodesk" / "src"
if str(PYNEURODESK_SRC) not in sys.path:
    sys.path.insert(0, str(PYNEURODESK_SRC))


BENCHMARK_PROGRAM = r'''
from __future__ import annotations

import argparse
import concurrent.futures
import json
import os
import platform
import random
import shutil
import time
from pathlib import Path


def now() -> float:
    return time.perf_counter()


def mb(value: float) -> float:
    return value / (1024 * 1024)


def timed(name: str, fn):
    start = now()
    extra = fn()
    duration = now() - start
    result = {"name": name, "seconds": duration}
    if extra:
        result.update(extra)
    return result


def partition(items, parts: int):
    parts = max(1, parts)
    return [items[index::parts] for index in range(parts)]


def run_parallel(items, workers: int, fn):
    chunks = [chunk for chunk in partition(items, workers) if chunk]
    if len(chunks) <= 1:
        return [fn(chunks[0] if chunks else [])]
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(chunks)) as executor:
        return list(executor.map(fn, chunks))


def write_all(path: Path, data: bytes) -> None:
    with path.open("wb") as handle:
        handle.write(data)


def read_all(path: Path) -> int:
    total = 0
    with path.open("rb") as handle:
        while True:
            chunk = handle.read(1024 * 1024)
            if not chunk:
                break
            total += len(chunk)
    return total


def fsync_file(path: Path) -> None:
    with path.open("rb") as handle:
        os.fsync(handle.fileno())


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--work-dir", required=True)
    parser.add_argument("--small-files", type=int, required=True)
    parser.add_argument("--small-size", type=int, required=True)
    parser.add_argument("--large-mb", type=int, required=True)
    parser.add_argument("--block-kb", type=int, required=True)
    parser.add_argument("--random-ops", type=int, required=True)
    parser.add_argument("--random-block-kb", type=int, required=True)
    parser.add_argument("--threads", type=int, default=1)
    parser.add_argument("--fsync", action="store_true")
    parser.add_argument(
        "--only",
        choices=[
            "small_create",
            "small_stat",
            "small_read",
            "small_delete",
            "metadata_probe",
            "large_sequential_write",
            "large_sequential_read",
            "large_random_read",
            "large_random_write",
        ],
        action="append",
        help="Run only the named benchmark section. Can be repeated.",
    )
    parser.add_argument("--label", required=True)
    parser.add_argument("--metadata-rounds", type=int, default=5)
    args = parser.parse_args()

    root = Path(args.work_dir).resolve()
    if root.exists():
        shutil.rmtree(root)
    root.mkdir(parents=True)

    small_dir = root / "small"
    small_dir.mkdir()
    small_payload = b"x" * args.small_size
    small_paths = [small_dir / f"file-{index:06d}.bin" for index in range(args.small_files)]
    threads = max(1, args.threads)

    results = []
    only = set(args.only or [])

    def wants(name: str) -> bool:
        return not only or name in only

    if wants("small_create"):
        def small_create() -> dict[str, int]:
            def worker(paths):
                for path in paths:
                    write_all(path, small_payload)
            run_parallel(small_paths, threads, worker)
            return {"files": args.small_files, "bytes": args.small_files * args.small_size, "threads": threads}

        results.append(timed("small_create", small_create))

    if args.fsync and small_paths and wants("small_fsync_each"):
        def small_fsync_each() -> dict[str, int]:
            def worker(paths):
                for path in paths:
                    fsync_file(path)
            run_parallel(small_paths, threads, worker)
            return {"files": args.small_files, "threads": threads}

        results.append(timed("small_fsync_each", small_fsync_each))

    if wants("small_stat"):
        def small_stat() -> dict[str, int]:
            def worker(paths):
                return sum(path.stat().st_size for path in paths)
            total = sum(run_parallel(small_paths, threads, worker))
            return {"files": args.small_files, "bytes": total, "threads": threads}

        results.append(timed("small_stat", small_stat))

    if wants("small_read"):
        def small_read() -> dict[str, int]:
            def worker(paths):
                return sum(len(path.read_bytes()) for path in paths)
            total = sum(run_parallel(small_paths, threads, worker))
            return {"files": args.small_files, "bytes": total, "threads": threads}

        results.append(timed("small_read", small_read))

    if wants("metadata_probe"):
        if not all(path.exists() for path in small_paths):
            def worker(paths):
                for path in paths:
                    write_all(path, small_payload)
            run_parallel(small_paths, threads, worker)

        def metadata_probe() -> dict[str, int]:
            ops = 0
            for _ in range(args.metadata_rounds):
                names = sorted(path.name for path in small_dir.iterdir())
                ops += len(names)

                def worker(paths):
                    total = 0
                    for path in paths:
                        total += path.stat().st_size
                    return total
                total = sum(run_parallel(small_paths, threads, worker))
                if total != args.small_files * args.small_size:
                    raise RuntimeError(f"metadata total mismatch: {total}")
                ops += len(small_paths)
            return {"files": args.small_files, "ops": ops, "threads": threads, "rounds": args.metadata_rounds}

        results.append(timed("metadata_probe", metadata_probe))

    if wants("small_delete"):
        def small_delete() -> dict[str, int]:
            def worker(paths):
                for path in paths:
                    path.unlink()
            run_parallel(small_paths, threads, worker)
            return {"files": args.small_files, "threads": threads}

        results.append(timed("small_delete", small_delete))

    large_path = root / "large.bin"
    large_bytes = args.large_mb * 1024 * 1024
    block = b"a" * (args.block_kb * 1024)
    blocks = large_bytes // len(block)

    def large_write() -> dict[str, int]:
        with large_path.open("wb") as handle:
            for _ in range(blocks):
                handle.write(block)
            remaining = large_bytes - blocks * len(block)
            if remaining:
                handle.write(block[:remaining])
            if args.fsync:
                os.fsync(handle.fileno())
        return {"bytes": large_bytes}

    if wants("large_sequential_write") or wants("large_sequential_read") or wants("large_random_read") or wants("large_random_write"):
        if wants("large_sequential_write"):
            results.append(timed("large_sequential_write", large_write))
        else:
            large_write()
    if wants("large_sequential_read"):
        results.append(timed("large_sequential_read", lambda: {"bytes": read_all(large_path)}))

    random_block = b"r" * (args.random_block_kb * 1024)
    max_offset = max(0, large_bytes - len(random_block))
    offsets = [
        random.randrange(0, max_offset + 1) if max_offset else 0
        for _ in range(args.random_ops)
    ]

    def random_reads() -> dict[str, int]:
        def worker(chunk):
            total = 0
            with large_path.open("rb", buffering=0) as handle:
                fd = handle.fileno()
                for offset in chunk:
                    total += len(os.pread(fd, len(random_block), offset))
            return total
        total = sum(run_parallel(offsets, threads, worker))
        return {"ops": args.random_ops, "bytes": total, "threads": threads}

    def random_writes() -> dict[str, int]:
        def worker(chunk):
            with large_path.open("r+b", buffering=0) as handle:
                fd = handle.fileno()
                for offset in chunk:
                    os.pwrite(fd, random_block, offset)
                if args.fsync:
                    os.fsync(fd)
        run_parallel(offsets, threads, worker)
        return {"ops": args.random_ops, "bytes": args.random_ops * len(random_block), "threads": threads}

    if wants("large_random_read"):
        results.append(timed("large_random_read", random_reads))
    if wants("large_random_write"):
        results.append(timed("large_random_write", random_writes))

    for result in results:
        seconds = result["seconds"]
        bytes_value = result.get("bytes")
        ops_value = result.get("ops") or result.get("files")
        if bytes_value and seconds > 0:
            result["mib_per_second"] = mb(bytes_value) / seconds
        if ops_value and seconds > 0:
            result["ops_per_second"] = ops_value / seconds

    print(json.dumps({
        "label": args.label,
        "platform": platform.platform(),
        "python": platform.python_version(),
        "work_dir": str(root),
        "parameters": {
            "small_files": args.small_files,
            "small_size": args.small_size,
            "large_mb": args.large_mb,
            "block_kb": args.block_kb,
            "random_ops": args.random_ops,
            "random_block_kb": args.random_block_kb,
            "threads": threads,
            "fsync": args.fsync,
            "only": sorted(only),
            "metadata_rounds": args.metadata_rounds,
        },
        "results": results,
    }, sort_keys=True))


if __name__ == "__main__":
    main()
'''


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Benchmark filesystem workloads on the host and inside CrumbleCracker."
    )
    parser.add_argument("--image-name", default="fsbench-python", help="CrumbleCracker image cache name")
    parser.add_argument(
        "--image-source",
        default="python:3.12-alpine",
        help="OCI image, local .simg/.sif, or CVMFS URL containing python3",
    )
    parser.add_argument("--cache-dir", type=Path, default=REPO_ROOT / ".tmp-fs-benchmark" / "cache")
    parser.add_argument("--work-dir", type=Path, default=REPO_ROOT / ".tmp-fs-benchmark" / "work")
    parser.add_argument("--ccvm", type=Path, default=None, help="Path to ccvm; built automatically when omitted")
    parser.add_argument("--no-build-ccvm", action="store_true", help="Do not build ccvm automatically")
    parser.add_argument("--memory-mb", type=int, default=2048)
    parser.add_argument("--cpus", type=int, default=None)
    parser.add_argument("--boot-timeout", type=float, default=60.0)
    parser.add_argument("--small-files", type=int, default=5000)
    parser.add_argument("--small-size", type=int, default=1024)
    parser.add_argument("--large-mb", type=int, default=256)
    parser.add_argument("--block-kb", type=int, default=1024)
    parser.add_argument("--random-ops", type=int, default=8192)
    parser.add_argument("--random-block-kb", type=int, default=4)
    parser.add_argument("--threads", type=int, default=1, help="Filesystem worker threads inside each benchmark process")
    parser.add_argument("--metadata-rounds", type=int, default=5)
    parser.add_argument("--fsync", action="store_true", help="Call fsync during write tests")
    parser.add_argument(
        "--virtiofs-async",
        choices=["on", "off"],
        default="on",
        help="Enable or disable asynchronous virtiofs dispatch in ccvm",
    )
    parser.add_argument(
        "--virtiofs-workers",
        type=int,
        default=None,
        help="Set CCX3_VIRTIOFS_WORKERS for the guest benchmark",
    )
    parser.add_argument(
        "--virtiofs-cache",
        choices=["normal", "strict", "aggressive"],
        default=None,
        help="Set CCX3_VIRTIOFS_CACHE for the guest benchmark",
    )
    parser.add_argument(
        "--virtiofs-writeback",
        action="store_true",
        help="Enable CCX3_VIRTIOFS_WRITEBACK for the guest benchmark",
    )
    parser.add_argument(
        "--virtiofs-kick-poll",
        choices=["on", "off", "default"],
        default="default",
        help="Set CCX3_VIRTIOFS_KICK_POLL for the guest benchmark",
    )
    parser.add_argument("--virtiofs-kick-poll-idle", default=None)
    parser.add_argument("--virtiofs-kick-poll-sleep", default=None)
    parser.add_argument(
        "--virtiofs-direct-memory",
        choices=["on", "off", "default"],
        default="default",
        help="Set CCX3_VIRTIOFS_DIRECT_MEMORY for the guest benchmark",
    )
    parser.add_argument(
        "--share-cache",
        choices=["strict", "normal", "aggressive"],
        default="strict",
        help="Cache policy for the benchmark share mount",
    )
    parser.add_argument(
        "--only",
        action="append",
        help="Run only the named benchmark section. Can be repeated. Example: --only small_create",
    )
    parser.add_argument(
        "--pprof-cpu-profile",
        type=Path,
        default=None,
        help="Write a Go CPU profile from ccvm while the guest benchmark runs",
    )
    parser.add_argument(
        "--pprof-mutex-profile",
        type=Path,
        default=None,
        help="Write a Go mutex profile from ccvm after the guest benchmark runs",
    )
    parser.add_argument(
        "--pprof-block-profile",
        type=Path,
        default=None,
        help="Write a Go block profile from ccvm after the guest benchmark runs",
    )
    parser.add_argument("--pprof-seconds", type=int, default=35, help="CPU profile duration in seconds")
    parser.add_argument("--shutdown-daemon", action="store_true", help="Stop the ccvm daemon after the guest run")
    parser.add_argument("--keep-work-dir", action="store_true")
    parser.add_argument("--output", type=Path, default=REPO_ROOT / "fs_benchmark_results.json")
    return parser.parse_args()


def source_type(source: str) -> str:
    lowered = source.lower()
    if lowered.endswith((".simg", ".sif")):
        return "simg"
    if lowered.startswith(("cvmfs://", "http+cvmfs://")) or "/cvmfs/" in lowered:
        return "cvmfs"
    return "oci"


def ensure_ccvm(args: argparse.Namespace) -> Path | None:
    if args.ccvm is not None:
        path = args.ccvm.expanduser().resolve()
        if not path.exists():
            raise FileNotFoundError(path)
        os.environ["PYNEURODESK_CCVM"] = str(path)
        return path
    for env_name in ("PYNEURODESK_CCVM", "CCX3_CCVM", "CCVM_BINARY"):
        value = os.environ.get(env_name)
        if value and Path(value).expanduser().exists():
            return Path(value).expanduser().resolve()
    if args.no_build_ccvm:
        return None
    out = REPO_ROOT / ".tmp-fs-benchmark" / "bin" / ("ccvm.exe" if os.name == "nt" else "ccvm")
    out.parent.mkdir(parents=True, exist_ok=True)
    print(f"[fsbench] building ccvm -> {out}", file=sys.stderr)
    build_guestinit_payloads()
    env = os.environ.copy()
    env["CGO_ENABLED"] = "0"
    subprocess.run(["go", "build", "-o", str(out), "./cmd/ccvm"], cwd=REPO_ROOT, env=env, check=True)
    os.environ["PYNEURODESK_CCVM"] = str(out)
    return out


def build_guestinit_payloads() -> None:
    build_dir = REPO_ROOT / "build"
    build_dir.mkdir(parents=True, exist_ok=True)
    for goarch in ("arm64", "amd64"):
        out = build_dir / f"init-linux-{goarch}"
        embed = REPO_ROOT / "internal" / "guestinit" / f"guest-init-linux-{goarch}"
        env = os.environ.copy()
        env.update({"CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": goarch})
        subprocess.run(["go", "build", "-o", str(out), "./internal/cmd/init"], cwd=REPO_ROOT, env=env, check=True)
        embed.write_bytes(out.read_bytes())
    goarch = host_goarch()
    for bsd in ("openbsd", "freebsd", "netbsd"):
        out = build_dir / f"guest-init-{bsd}-{goarch}"
        embed = REPO_ROOT / "internal" / bsd / "guestinit" / f"guest-init-{bsd}-{goarch}"
        env = os.environ.copy()
        env.update({"CGO_ENABLED": "0", "GOOS": bsd, "GOARCH": goarch})
        subprocess.run(["go", "build", "-o", str(out), f"./internal/cmd/{bsd}-init"], cwd=REPO_ROOT, env=env, check=True)
        embed.write_bytes(out.read_bytes())


def host_goarch() -> str:
    machine = platform.machine().lower()
    if machine in {"x86_64", "amd64"}:
        return "amd64"
    if machine in {"aarch64", "arm64"}:
        return "arm64"
    raise RuntimeError(f"unsupported host architecture for bundled BSD guest init: {platform.machine()}")


def benchmark_args(label: str, work_dir: str, args: argparse.Namespace) -> list[str]:
    command = [
        "--label", label,
        "--work-dir", work_dir,
        "--small-files", str(args.small_files),
        "--small-size", str(args.small_size),
        "--large-mb", str(args.large_mb),
        "--block-kb", str(args.block_kb),
        "--random-ops", str(args.random_ops),
        "--random-block-kb", str(args.random_block_kb),
        "--threads", str(args.threads),
        "--metadata-rounds", str(args.metadata_rounds),
    ]
    if args.fsync:
        command.append("--fsync")
    for name in args.only or []:
        command.extend(["--only", name])
    return command


def start_cpu_profile(base_url: str, output: Path, seconds: int) -> threading.Thread:
    output.parent.mkdir(parents=True, exist_ok=True)

    def worker() -> None:
        url = f"{base_url.rstrip('/')}/debug/pprof/profile?seconds={seconds}"
        with urllib.request.urlopen(url, timeout=seconds + 15) as response:
            output.write_bytes(response.read())

    thread = threading.Thread(target=worker, name="ccvm-pprof-cpu", daemon=True)
    thread.start()
    return thread


def fetch_pprof_profile(base_url: str, profile_name: str, output: Path) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    url = f"{base_url.rstrip('/')}/debug/pprof/{profile_name}?debug=0"
    with urllib.request.urlopen(url, timeout=30) as response:
        output.write_bytes(response.read())


def fetch_virtiofs_stats(base_url: str) -> Any:
    url = f"{base_url.rstrip('/')}/debug/virtiofs"
    try:
        with urllib.request.urlopen(url, timeout=10) as response:
            return json.loads(response.read().decode("utf-8"))
    except Exception as exc:
        return {"error": str(exc)}


def fetch_exit_stats(base_url: str) -> Any:
    url = f"{base_url.rstrip('/')}/debug/exits"
    try:
        with urllib.request.urlopen(url, timeout=10) as response:
            return json.loads(response.read().decode("utf-8"))
    except Exception as exc:
        return {"error": str(exc)}


def apply_virtiofs_env(args: argparse.Namespace) -> dict[str, Any]:
    os.environ["CCX3_VIRTIOFS_ASYNC"] = "1" if args.virtiofs_async == "on" else "0"
    if args.virtiofs_workers is not None:
        os.environ["CCX3_VIRTIOFS_WORKERS"] = str(args.virtiofs_workers)
    else:
        os.environ.pop("CCX3_VIRTIOFS_WORKERS", None)
    if args.virtiofs_cache is not None:
        os.environ["CCX3_VIRTIOFS_CACHE"] = args.virtiofs_cache
    else:
        os.environ.pop("CCX3_VIRTIOFS_CACHE", None)
    if args.virtiofs_writeback:
        os.environ["CCX3_VIRTIOFS_WRITEBACK"] = "1"
    else:
        os.environ.pop("CCX3_VIRTIOFS_WRITEBACK", None)
    if args.virtiofs_kick_poll == "default":
        os.environ.pop("CCX3_VIRTIOFS_KICK_POLL", None)
    else:
        os.environ["CCX3_VIRTIOFS_KICK_POLL"] = "1" if args.virtiofs_kick_poll == "on" else "0"
    if args.virtiofs_kick_poll_idle:
        os.environ["CCX3_VIRTIOFS_KICK_POLL_IDLE"] = args.virtiofs_kick_poll_idle
    else:
        os.environ.pop("CCX3_VIRTIOFS_KICK_POLL_IDLE", None)
    if args.virtiofs_kick_poll_sleep:
        os.environ["CCX3_VIRTIOFS_KICK_POLL_SLEEP"] = args.virtiofs_kick_poll_sleep
    else:
        os.environ.pop("CCX3_VIRTIOFS_KICK_POLL_SLEEP", None)
    if args.virtiofs_direct_memory == "default":
        os.environ.pop("CCX3_VIRTIOFS_DIRECT_MEMORY", None)
    else:
        os.environ["CCX3_VIRTIOFS_DIRECT_MEMORY"] = "1" if args.virtiofs_direct_memory == "on" else "0"
    return {
        "async": args.virtiofs_async,
        "workers": args.virtiofs_workers,
        "cache": args.virtiofs_cache or "normal",
        "share_cache": args.share_cache,
        "writeback": args.virtiofs_writeback,
        "kick_poll": args.virtiofs_kick_poll,
        "kick_poll_idle": args.virtiofs_kick_poll_idle,
        "kick_poll_sleep": args.virtiofs_kick_poll_sleep,
        "direct_memory": args.virtiofs_direct_memory,
    }


def diff_virtiofs_stats(before: Any, after: Any) -> Any:
    if not isinstance(before, list) or not isinstance(after, list):
        return None
    before_by_tag = {item.get("tag", ""): item for item in before if isinstance(item, dict)}
    out = []
    for after_item in after:
        if not isinstance(after_item, dict):
            continue
        tag = after_item.get("tag", "")
        before_item = before_by_tag.get(tag, {})
        delta = {
            "tag": tag,
            "mmio_reads": int(after_item.get("mmio_reads", 0)) - int(before_item.get("mmio_reads", 0)),
            "mmio_writes": int(after_item.get("mmio_writes", 0)) - int(before_item.get("mmio_writes", 0)),
            "fuse_requests": int(after_item.get("fuse_requests", 0)) - int(before_item.get("fuse_requests", 0)),
            "interrupt_raises": int(after_item.get("interrupt_raises", 0)) - int(before_item.get("interrupt_raises", 0)),
            "irq_transitions": int(after_item.get("irq_transitions", 0)) - int(before_item.get("irq_transitions", 0)),
            "queue_notifies": diff_numeric_list(
                before_item.get("queue_notifies", []),
                after_item.get("queue_notifies", []),
            ),
            "kick_poll_loops": int(after_item.get("kick_poll_loops", 0)) - int(before_item.get("kick_poll_loops", 0)),
            "kick_poll_hits": int(after_item.get("kick_poll_hits", 0)) - int(before_item.get("kick_poll_hits", 0)),
            "kick_poll_misses": int(after_item.get("kick_poll_misses", 0)) - int(before_item.get("kick_poll_misses", 0)),
            "kick_poll_works": int(after_item.get("kick_poll_works", 0)) - int(before_item.get("kick_poll_works", 0)),
            "fuse_ops": [],
            "stages": [],
        }
        delta["stages"] = diff_timing_stats(
            before_item.get("stages", []),
            after_item.get("stages", []),
            "name",
        )
        before_ops = {
            op.get("opcode"): op
            for op in before_item.get("fuse_ops", [])
            if isinstance(op, dict)
        }
        for after_op in after_item.get("fuse_ops", []):
            if not isinstance(after_op, dict):
                continue
            opcode = after_op.get("opcode")
            before_op = before_ops.get(opcode, {})
            count = int(after_op.get("count", 0)) - int(before_op.get("count", 0))
            total_nanos = int(after_op.get("total_nanos", 0)) - int(before_op.get("total_nanos", 0))
            max_nanos = int(after_op.get("max_nanos", 0))
            delta["fuse_ops"].append({
                "opcode": opcode,
                "name": after_op.get("name"),
                "count": count,
                "total_nanos": total_nanos,
                "average_nanos": total_nanos // count if count else 0,
                "max_nanos_after": max_nanos,
            })
        delta["fuse_ops"].sort(key=lambda op: op["count"], reverse=True)
        out.append(delta)
    return out


def diff_timing_stats(before_items: Any, after_items: Any, key_name: str) -> list[dict[str, Any]]:
    if not isinstance(before_items, list) or not isinstance(after_items, list):
        return []
    before_by_key = {
        item.get(key_name): item
        for item in before_items
        if isinstance(item, dict)
    }
    out = []
    for after_item in after_items:
        if not isinstance(after_item, dict):
            continue
        key = after_item.get(key_name)
        before_item = before_by_key.get(key, {})
        count = int(after_item.get("count", 0)) - int(before_item.get("count", 0))
        total_nanos = int(after_item.get("total_nanos", 0)) - int(before_item.get("total_nanos", 0))
        item = {
            key_name: key,
            "count": count,
            "total_nanos": total_nanos,
            "average_nanos": total_nanos // count if count else 0,
            "max_nanos_after": int(after_item.get("max_nanos", 0)),
        }
        out.append(item)
    out.sort(key=lambda item: item["total_nanos"], reverse=True)
    return out


def diff_numeric_list(before: Any, after: Any) -> list[int]:
    if not isinstance(before, list):
        before = []
    if not isinstance(after, list):
        after = []
    count = max(len(before), len(after))
    return [
        int(after[index] if index < len(after) else 0) - int(before[index] if index < len(before) else 0)
        for index in range(count)
    ]


def diff_exit_stats(before: Any, after: Any) -> list[dict[str, Any]]:
    if not isinstance(before, dict) or not isinstance(after, dict):
        return []
    out = []
    for name, after_item in after.items():
        if not isinstance(after_item, dict):
            continue
        before_item = before.get(name, {})
        if not isinstance(before_item, dict):
            before_item = {}
        count = int(after_item.get("count", 0)) - int(before_item.get("count", 0))
        total_nanos = int(after_item.get("total_nanos", 0)) - int(before_item.get("total_nanos", 0))
        out.append({
            "name": name,
            "count": count,
            "total_nanos": total_nanos,
            "average_nanos": total_nanos // count if count else 0,
            "max_nanos_after": int(after_item.get("max_nanos", 0)),
        })
    out.sort(key=lambda item: item["total_nanos"], reverse=True)
    return out


def run_host(args: argparse.Namespace) -> dict[str, Any]:
    host_root = args.work_dir / "host"
    print(f"[fsbench] running host benchmark in {host_root}", file=sys.stderr)
    proc = subprocess.run(
        [sys.executable, "-c", BENCHMARK_PROGRAM, *benchmark_args("host", str(host_root), args)],
        text=True,
        capture_output=True,
        check=True,
    )
    return json.loads(proc.stdout)


def run_guest(args: argparse.Namespace) -> dict[str, Any]:
    from pyneurodesk.api import start_daemon_for_cache_dir
    from pyneurodesk.client import PyNeurodeskClient
    from pyneurodesk.models import ImageSource, ImportImageRequest, ShareMount

    guest_host_root = args.work_dir / "guest-share"
    guest_host_root.mkdir(parents=True, exist_ok=True)
    guest_script = guest_host_root / "fsbench_guest.py"
    guest_script.write_text(BENCHMARK_PROGRAM)
    mount = "/.share/fsbench"
    guest_script_path = f"{mount}/fsbench_guest.py"
    guest_work = f"{mount}/bench"

    if (
        args.pprof_cpu_profile is not None
        or args.pprof_mutex_profile is not None
        or args.pprof_block_profile is not None
    ):
        os.environ["CCX3_PPROF"] = "1"
    os.environ["CCX3_EXIT_TIMING"] = "1"
    virtiofs_config = apply_virtiofs_env(args)
    print(f"[fsbench] starting ccvm daemon with cache {args.cache_dir}", file=sys.stderr)
    state = start_daemon_for_cache_dir(args.cache_dir)
    client = PyNeurodeskClient(state.base_url)
    try:
        if client.get_image(args.image_name) is None:
            print(f"[fsbench] importing {args.image_name} from {args.image_source}", file=sys.stderr)
            client.import_image(
                args.image_name,
                ImportImageRequest(
                    source=ImageSource(type=source_type(args.image_source), path=args.image_source)
                ),
            )
        print("[fsbench] preparing kernel, emulator, and image metadata", file=sys.stderr)
        client.download_kernel()
        client.prepare_image_emulator(args.image_name)
        client.prepare_image_metadata(args.image_name)
        print(f"[fsbench] starting VM for {args.image_name}", file=sys.stderr)
        client.ensure_instance(
            args.image_name,
            timeout=args.boot_timeout,
            memory_mb=args.memory_mb,
            cpus=args.cpus,
        )
        stats_before = fetch_virtiofs_stats(str(client._client.base_url))
        exits_before = fetch_exit_stats(str(client._client.base_url))
        print(f"[fsbench] running guest benchmark in {guest_work}", file=sys.stderr)
        profile_thread = None
        if args.pprof_cpu_profile is not None:
            print(
                f"[fsbench] collecting ccvm CPU profile for {args.pprof_seconds}s -> {args.pprof_cpu_profile}",
                file=sys.stderr,
            )
            profile_thread = start_cpu_profile(str(client._client.base_url), args.pprof_cpu_profile, args.pprof_seconds)
            time.sleep(0.5)
        result = client.run(
            args.image_name,
            ["python3", guest_script_path, *benchmark_args("crumblecracker", guest_work, args)],
            shares=[ShareMount(source=str(guest_host_root.resolve()), mount=mount, writable=True, cache=args.share_cache)],
            timeout=None,
        )
        if profile_thread is not None:
            profile_thread.join(timeout=args.pprof_seconds + 20)
        if result.exit_code != 0:
            raise RuntimeError(f"guest benchmark failed with exit {result.exit_code}:\n{result.output}")
        if args.pprof_mutex_profile is not None:
            print(f"[fsbench] collecting ccvm mutex profile -> {args.pprof_mutex_profile}", file=sys.stderr)
            fetch_pprof_profile(str(client._client.base_url), "mutex", args.pprof_mutex_profile)
        if args.pprof_block_profile is not None:
            print(f"[fsbench] collecting ccvm block profile -> {args.pprof_block_profile}", file=sys.stderr)
            fetch_pprof_profile(str(client._client.base_url), "block", args.pprof_block_profile)
        payload = json.loads(result.output)
        stats_after = fetch_virtiofs_stats(str(client._client.base_url))
        exits_after = fetch_exit_stats(str(client._client.base_url))
        payload["virtiofs_stats_before"] = stats_before
        payload["virtiofs_stats_after"] = stats_after
        payload["virtiofs_stats_delta"] = diff_virtiofs_stats(stats_before, stats_after)
        payload["exit_stats_before"] = exits_before
        payload["exit_stats_after"] = exits_after
        payload["exit_stats_delta"] = diff_exit_stats(exits_before, exits_after)
        payload["virtiofs_config"] = virtiofs_config
        return payload
    finally:
        if args.shutdown_daemon:
            try:
                urllib.request.urlopen(
                    urllib.request.Request(
                        f"{str(client._client.base_url).rstrip('/')}/shutdown",
                        method="POST",
                        data=b"{}",
                        headers={"Content-Type": "application/json"},
                    ),
                    timeout=5,
                ).read()
            except Exception:
                pass
        client.close()


def summarize(host: dict[str, Any], guest: dict[str, Any]) -> list[dict[str, Any]]:
    host_by_name = {item["name"]: item for item in host["results"]}
    out = []
    for guest_item in guest["results"]:
        name = guest_item["name"]
        host_item = host_by_name[name]
        host_seconds = float(host_item["seconds"])
        guest_seconds = float(guest_item["seconds"])
        ratio = guest_seconds / host_seconds if host_seconds > 0 else None
        out.append({
            "name": name,
            "host_seconds": host_seconds,
            "crumblecracker_seconds": guest_seconds,
            "ratio": ratio,
            "overhead_percent": (ratio - 1.0) * 100.0 if ratio is not None else None,
            "host_mib_per_second": host_item.get("mib_per_second"),
            "crumblecracker_mib_per_second": guest_item.get("mib_per_second"),
            "host_ops_per_second": host_item.get("ops_per_second"),
            "crumblecracker_ops_per_second": guest_item.get("ops_per_second"),
        })
    return out


def print_summary(summary: list[dict[str, Any]]) -> None:
    print()
    print("Filesystem benchmark summary")
    print("----------------------------")
    print(f"{'test':<28} {'host(s)':>10} {'cc(s)':>10} {'ratio':>9} {'overhead':>10}")
    for item in summary:
        ratio = item["ratio"]
        overhead = item["overhead_percent"]
        print(
            f"{item['name']:<28} "
            f"{item['host_seconds']:>10.3f} "
            f"{item['crumblecracker_seconds']:>10.3f} "
            f"{ratio:>9.2f} "
            f"{overhead:>9.1f}%"
        )


def print_virtiofs_summary(guest: dict[str, Any]) -> None:
    delta = guest.get("virtiofs_stats_delta")
    if not isinstance(delta, list):
        return
    print()
    print("Virtiofs summary")
    print("----------------")
    config = guest.get("virtiofs_config", {})
    if isinstance(config, dict):
        print(
            "config: "
            f"async={config.get('async')} "
            f"workers={config.get('workers') or 'default'} "
            f"cache={config.get('cache')} "
            f"share_cache={config.get('share_cache')} "
            f"writeback={config.get('writeback')} "
            f"kick_poll={config.get('kick_poll')} "
            f"direct_memory={config.get('direct_memory')}"
        )
    for item in delta:
        if not isinstance(item, dict):
            continue
        print(
            f"{item.get('tag', '<unknown>')}: "
            f"requests={item.get('fuse_requests', 0)} "
            f"queues={item.get('queue_notifies', [])}"
        )
        if item.get("kick_poll_loops"):
            print(
                "  kick-poll "
                f"loops={item.get('kick_poll_loops', 0)} "
                f"hits={item.get('kick_poll_hits', 0)} "
                f"misses={item.get('kick_poll_misses', 0)} "
                f"works={item.get('kick_poll_works', 0)}"
            )
        ops = [
            op for op in item.get("fuse_ops", [])
            if isinstance(op, dict) and int(op.get("count", 0)) > 0
        ][:8]
        for op in ops:
            avg_us = int(op.get("average_nanos", 0)) / 1000.0
            print(f"  {op.get('name'):<10} {op.get('count'):>8} avg={avg_us:>8.1f}us")
    exits = [
        item for item in guest.get("exit_stats_delta", [])
        if isinstance(item, dict) and int(item.get("count", 0)) > 0
    ][:8]
    if exits:
        print()
        print("Exit summary")
        print("------------")
        for item in exits:
            avg_us = int(item.get("average_nanos", 0)) / 1000.0
            total_ms = int(item.get("total_nanos", 0)) / 1_000_000.0
            print(f"{item.get('name'):<36} {item.get('count'):>9} avg={avg_us:>7.1f}us total={total_ms:>8.1f}ms")


def main() -> None:
    args = parse_args()
    args.cache_dir = args.cache_dir.expanduser().resolve()
    args.work_dir = args.work_dir.expanduser().resolve()
    args.output = args.output.expanduser().resolve()
    args.work_dir.mkdir(parents=True, exist_ok=True)

    ensure_ccvm(args)
    started = time.time()
    host = run_host(args)
    guest = run_guest(args)
    summary = summarize(host, guest)
    payload = {
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "elapsed_seconds": time.time() - started,
        "host": host,
        "crumblecracker": guest,
        "summary": summary,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n")
    print_summary(summary)
    print_virtiofs_summary(guest)
    print(f"\nwrote {args.output}")

    if not args.keep_work_dir:
        shutil.rmtree(args.work_dir / "host", ignore_errors=True)
        shutil.rmtree(args.work_dir / "guest-share", ignore_errors=True)


if __name__ == "__main__":
    main()
