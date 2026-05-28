#!/usr/bin/env python3
"""Run a short virtiofs exit-count sweep.

This is intentionally a developer benchmark. It keeps the guest workload small
enough to finish quickly once the VM is warm, but it stresses the operations
that currently dominate vsh usage: metadata walks plus one sequential read.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
import time
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[1]


VARIANTS: list[tuple[str, list[str]]] = [
    ("baseline", []),
    ("share-cache-aggressive", ["--share-cache", "aggressive"]),
    ("share-cache-normal", ["--share-cache", "normal"]),
    ("workers-1", ["--virtiofs-workers", "1"]),
    ("workers-2", ["--virtiofs-workers", "2"]),
    ("workers-4", ["--virtiofs-workers", "4"]),
    ("workers-8", ["--virtiofs-workers", "8"]),
    ("sync-dispatch", ["--virtiofs-async", "off"]),
    ("writeback", ["--virtiofs-writeback"]),
    ("kick-poll-default", ["--virtiofs-kick-poll", "on"]),
    (
        "kick-poll-short",
        [
            "--virtiofs-kick-poll",
            "on",
            "--virtiofs-kick-poll-idle",
            "100us",
            "--virtiofs-kick-poll-sleep",
            "0",
        ],
    ),
]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Sweep short virtiofs exit benchmark variants.")
    parser.add_argument("--cache-dir", type=Path, default=REPO_ROOT / ".tmp-virtiofs-exit-sweep" / "cache")
    parser.add_argument("--work-dir", type=Path, default=REPO_ROOT / ".tmp-virtiofs-exit-sweep" / "work")
    parser.add_argument("--output-dir", type=Path, default=REPO_ROOT / ".tmp-virtiofs-exit-sweep" / "results")
    parser.add_argument("--ccvm", type=Path, default=None)
    parser.add_argument("--image-name", default="fsbench-python")
    parser.add_argument("--image-source", default="python:3.12-alpine")
    parser.add_argument("--small-files", type=int, default=384)
    parser.add_argument("--metadata-rounds", type=int, default=8)
    parser.add_argument("--large-mb", type=int, default=16)
    parser.add_argument("--cpus", type=int, default=None)
    parser.add_argument("--limit", type=int, default=0, help="Run only the first N variants.")
    return parser.parse_args()


def result_by_name(payload: dict[str, Any], name: str) -> dict[str, Any]:
    for item in payload.get("crumblecracker", {}).get("results", []):
        if item.get("name") == name:
            return item
    return {}


def exit_totals(payload: dict[str, Any]) -> tuple[int, int, int]:
    total_count = 0
    total_nanos = 0
    max_nanos = 0
    for item in payload.get("crumblecracker", {}).get("exit_stats_delta", []):
        if not isinstance(item, dict):
            continue
        if not str(item.get("name", "")).startswith("data_abort.virtiofs"):
            continue
        count = int(item.get("count", 0))
        nanos = int(item.get("total_nanos", 0))
        total_count += count
        total_nanos += nanos
        max_nanos = max(max_nanos, int(item.get("max_nanos_after", 0)))
    avg_nanos = total_nanos // total_count if total_count else 0
    return total_count, avg_nanos, max_nanos


def virtiofs_totals(payload: dict[str, Any]) -> tuple[int, int, int]:
    requests = 0
    mmio = 0
    notifies = 0
    for item in payload.get("crumblecracker", {}).get("virtiofs_stats_delta", []):
        if not isinstance(item, dict):
            continue
        requests += int(item.get("fuse_requests", 0))
        mmio += int(item.get("mmio_reads", 0)) + int(item.get("mmio_writes", 0))
        notifies += sum(int(value) for value in item.get("queue_notifies", []))
    return requests, mmio, notifies


def run_variant(args: argparse.Namespace, name: str, extra: list[str]) -> dict[str, Any]:
    output = args.output_dir / f"{name}.json"
    cmd = [
        sys.executable,
        str(REPO_ROOT / "tools" / "fs_benchmark.py"),
        "--cache-dir",
        str(args.cache_dir),
        "--work-dir",
        str(args.work_dir / name),
        "--output",
        str(output),
        "--image-name",
        args.image_name,
        "--image-source",
        args.image_source,
        "--small-files",
        str(args.small_files),
        "--small-size",
        "64",
        "--metadata-rounds",
        str(args.metadata_rounds),
        "--large-mb",
        str(args.large_mb),
        "--block-kb",
        "1024",
        "--random-ops",
        "128",
        "--random-block-kb",
        "4",
        "--only",
        "metadata_probe",
        "--only",
        "large_sequential_read",
        "--shutdown-daemon",
    ]
    if args.ccvm is not None:
        cmd.extend(["--ccvm", str(args.ccvm)])
    if args.cpus is not None:
        cmd.extend(["--cpus", str(args.cpus)])
    cmd.extend(extra)

    print(f"[sweep] {name}: {' '.join(extra) if extra else 'default'}", flush=True)
    started = time.perf_counter()
    subprocess.run(cmd, cwd=REPO_ROOT, check=True)
    payload = json.loads(output.read_text())
    payload["variant"] = name
    payload["sweep_elapsed_seconds"] = time.perf_counter() - started
    return payload


def main() -> None:
    args = parse_args()
    args.cache_dir = args.cache_dir.expanduser().resolve()
    args.work_dir = args.work_dir.expanduser().resolve()
    args.output_dir = args.output_dir.expanduser().resolve()
    args.output_dir.mkdir(parents=True, exist_ok=True)

    variants = VARIANTS[: args.limit] if args.limit > 0 else VARIANTS
    rows = []
    for name, extra in variants:
        payload = run_variant(args, name, extra)
        metadata = result_by_name(payload, "metadata_probe")
        read = result_by_name(payload, "large_sequential_read")
        exit_count, exit_avg_ns, exit_max_ns = exit_totals(payload)
        fuse_requests, mmio, notifies = virtiofs_totals(payload)
        rows.append(
            {
                "variant": name,
                "metadata_seconds": float(metadata.get("seconds", 0)),
                "metadata_ops_per_second": float(metadata.get("ops_per_second", 0)),
                "read_mib_per_second": float(read.get("mib_per_second", 0)),
                "exit_count": exit_count,
                "exit_average_us": exit_avg_ns / 1000.0,
                "exit_max_ms": exit_max_ns / 1_000_000.0,
                "fuse_requests": fuse_requests,
                "virtiofs_mmio": mmio,
                "queue_notifies": notifies,
                "elapsed_seconds": float(payload.get("elapsed_seconds", 0)),
                "output": str((args.output_dir / f"{name}.json").resolve()),
            }
        )

    summary = args.output_dir / "summary.json"
    summary.write_text(json.dumps(rows, indent=2, sort_keys=True) + "\n")

    print()
    print("Virtiofs exit sweep")
    print("-------------------")
    print(
        f"{'variant':<20} {'meta(s)':>8} {'meta ops/s':>11} {'read MiB/s':>11} "
        f"{'exits':>10} {'exit avg':>9} {'mmio':>9} {'notifies':>9}"
    )
    for row in rows:
        print(
            f"{row['variant']:<20} "
            f"{row['metadata_seconds']:>8.3f} "
            f"{row['metadata_ops_per_second']:>11.0f} "
            f"{row['read_mib_per_second']:>11.1f} "
            f"{row['exit_count']:>10} "
            f"{row['exit_average_us']:>8.1f}us "
            f"{row['virtiofs_mmio']:>9} "
            f"{row['queue_notifies']:>9}"
        )
    print(f"\nwrote {summary}")


if __name__ == "__main__":
    main()
