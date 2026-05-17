#!/usr/bin/env python3
"""Plot matched PyNeurodesk fulltest durations from Actions artifacts."""

from __future__ import annotations

import argparse
import colorsys
import hashlib
import json
import re
from collections import Counter
from pathlib import Path
from typing import Iterable


DurationMap = dict[tuple[str, str], float]


TINYRANGE_PASS_RE = re.compile(r"^\[fulltest\] passed (.+?) \(([0-9.]+)s\)$")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Compare Neurocontainers and Tinyrange PyNeurodesk fulltest durations "
            "for tests that passed in both runs."
        )
    )
    parser.add_argument(
        "--neurocontainers-root",
        type=Path,
        required=False,
        help="Directory containing fulltest-results-* artifact directories.",
    )
    parser.add_argument(
        "--tinyrange-root",
        type=Path,
        required=False,
        help="Directory containing pyneurodesk-fulltest-* artifact directories.",
    )
    parser.add_argument(
        "--left-report",
        type=Path,
        help="Fulltest JSON report for the x-axis. Used instead of --neurocontainers-root when provided.",
    )
    parser.add_argument(
        "--right-report",
        type=Path,
        help="Fulltest JSON report for the y-axis. Used instead of --tinyrange-root when provided.",
    )
    parser.add_argument(
        "--left-label",
        default="Neurocontainers / Apptainer",
        help="Label for the x-axis dataset.",
    )
    parser.add_argument(
        "--right-label",
        default="Tinyrange",
        help="Label for the y-axis dataset.",
    )
    parser.add_argument(
        "--output",
        type=Path,
        default=Path("benchmark_times_scatter.png"),
        help="Output PNG path for the plain scatter plot.",
    )
    parser.add_argument(
        "--by-suite-output",
        type=Path,
        help="Also write a suite-colored scatter plot to this PNG path.",
    )
    parser.add_argument(
        "--title",
        default="Tinyrange vs Neurocontainers Fulltest Durations",
        help="Title for the plain scatter plot.",
    )
    parser.add_argument(
        "--suite-title",
        default="Tinyrange vs Neurocontainers Fulltest Durations by Suite",
        help="Title for the suite-colored scatter plot.",
    )
    return parser.parse_args()


def iter_files(root: Path, pattern: str, suffix: str) -> Iterable[Path]:
    if not root.exists():
        raise SystemExit(f"missing artifact directory: {root}")
    return sorted(path for path in root.glob(pattern) if path.name.endswith(suffix))


def load_neurocontainers(root: Path) -> DurationMap:
    durations: DurationMap = {}
    for path in iter_files(root, "fulltest-results-*/*.jsonl", ".jsonl"):
        suite = path.parent.name.removeprefix("fulltest-results-")
        for line in path.read_text(errors="replace").splitlines():
            if not line.strip():
                continue
            item = json.loads(line)
            if item.get("passed") is True:
                durations[(item.get("suite") or suite, item.get("test") or "")] = float(
                    item.get("duration") or 0
                )
    return durations


def load_tinyrange(root: Path) -> DurationMap:
    durations: DurationMap = {}
    for path in iter_files(root, "pyneurodesk-fulltest-*/*.log", ".log"):
        suite = path.name.removesuffix(".log")
        for line in path.read_text(errors="replace").splitlines():
            match = TINYRANGE_PASS_RE.match(line)
            if match:
                durations[(suite, match.group(1))] = float(match.group(2))
    return durations


def load_fulltest_report(path: Path) -> DurationMap:
    report = json.loads(path.read_text())
    suite = report.get("suite") or path.stem
    durations: DurationMap = {}
    for item in report.get("results", []):
        if item.get("passed") is True:
            item_suite = item.get("suite") or suite
            name = item.get("name") or ""
            duration = item.get("duration_seconds") or item.get("usage", {}).get("wall_seconds") or 0
            durations[(item_suite, name)] = float(duration)
    return durations


def matched_points(neurocontainers: DurationMap, tinyrange: DurationMap) -> list[tuple[float, float, str, str]]:
    common = sorted(set(neurocontainers) & set(tinyrange))
    points = [
        (neurocontainers[key], tinyrange[key], key[0], key[1])
        for key in common
        if neurocontainers[key] > 0 and tinyrange[key] > 0
    ]
    if not points:
        raise SystemExit("no common passing test durations found")
    return points


def suite_color(suite: str) -> tuple[float, float, float, float]:
    hue = int(hashlib.sha1(suite.encode()).hexdigest()[:8], 16) / 0xFFFFFFFF
    red, green, blue = colorsys.hsv_to_rgb(hue, 0.70, 0.82)
    return (red, green, blue, 0.62)


def axis_bounds(points: list[tuple[float, float, str, str]]) -> tuple[float, float]:
    x_values = [point[0] for point in points]
    y_values = [point[1] for point in points]
    lowest = min(min(x_values), min(y_values))
    highest = max(max(x_values), max(y_values))
    return max(0.03, lowest * 0.75), highest * 1.35


def summary_text(
    points: list[tuple[float, float, str, str]],
    ratio: float,
    left_label: str,
    right_label: str,
    include_suites: bool = False,
) -> str:
    x_values = [point[0] for point in points]
    y_values = [point[1] for point in points]
    lines = [f"Common passing tests: {len(points):,}"]
    if include_suites:
        lines.append(f"Suites: {len({point[2] for point in points})}")
    lines.extend(
        [
            f"{left_label} total: {sum(x_values):,.0f}s",
            f"{right_label} total: {sum(y_values):,.0f}s",
            f"Weighted overhead: {(ratio - 1) * 100:.1f}%",
        ]
    )
    return "\n".join(lines)


def setup_matplotlib():
    try:
        import matplotlib

        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
        from matplotlib.lines import Line2D
    except Exception as exc:  # pragma: no cover - exercised by missing local dependency.
        raise SystemExit(f"matplotlib unavailable: {exc}") from exc

    plt.rcParams.update(
        {
            "font.size": 10,
            "axes.titlesize": 15,
            "axes.labelsize": 11,
            "figure.facecolor": "white",
            "axes.facecolor": "#fbfbfd",
        }
    )
    return plt, Line2D


def decorate_axes(ax, points: list[tuple[float, float, str, str]], ratio: float, title: str, left_label: str, right_label: str) -> None:
    low, high = axis_bounds(points)
    line = [low, high]
    ax.plot(line, line, color="#111827", lw=1.4, linestyle="--", label="equal time")
    ax.plot(line, [value * ratio for value in line], color="#dc2626", lw=1.6, label=f"weighted ratio {ratio:.2f}x")
    ax.set_xscale("log")
    ax.set_yscale("log")
    ax.set_xlim(low, high)
    ax.set_ylim(low, high)
    ax.grid(True, which="major", color="#d1d5db", linewidth=0.7, alpha=0.8)
    ax.grid(True, which="minor", color="#e5e7eb", linewidth=0.4, alpha=0.55)
    ax.set_title(title)
    ax.set_xlabel(f"{left_label} duration (seconds, log scale)")
    ax.set_ylabel(f"{right_label} duration (seconds, log scale)")


def plot_plain(points: list[tuple[float, float, str, str]], output: Path, title: str, left_label: str, right_label: str) -> None:
    plt, _ = setup_matplotlib()
    x_values = [point[0] for point in points]
    y_values = [point[1] for point in points]
    ratio = sum(y_values) / sum(x_values)

    fig, ax = plt.subplots(figsize=(11, 8), dpi=180)
    ax.scatter(
        x_values,
        y_values,
        s=13,
        alpha=0.35,
        c="#2563eb",
        edgecolors="none",
        label=f"{len(points):,} tests passed in both",
    )
    decorate_axes(ax, points, ratio, title, left_label, right_label)
    ax.legend(loc="upper left", frameon=True, facecolor="white", edgecolor="#d1d5db")
    ax.text(
        0.98,
        0.04,
        summary_text(points, ratio, left_label, right_label),
        transform=ax.transAxes,
        ha="right",
        va="bottom",
        bbox=dict(boxstyle="round,pad=0.45", facecolor="white", edgecolor="#d1d5db", alpha=0.95),
    )
    output.parent.mkdir(parents=True, exist_ok=True)
    fig.tight_layout()
    fig.savefig(output)


def plot_by_suite(points: list[tuple[float, float, str, str]], output: Path, title: str, left_label: str, right_label: str) -> None:
    plt, line_2d = setup_matplotlib()
    x_values = [point[0] for point in points]
    y_values = [point[1] for point in points]
    ratio = sum(y_values) / sum(x_values)
    suites = sorted({point[2] for point in points})
    colors = {suite: suite_color(suite) for suite in suites}

    fig, ax = plt.subplots(figsize=(12, 8.5), dpi=180)
    for suite in suites:
        suite_x = [point[0] for point in points if point[2] == suite]
        suite_y = [point[1] for point in points if point[2] == suite]
        ax.scatter(suite_x, suite_y, s=13, alpha=0.62, c=[colors[suite]], edgecolors="none")

    decorate_axes(ax, points, ratio, title, left_label, right_label)
    ax.text(
        0.98,
        0.04,
        summary_text(points, ratio, left_label, right_label, include_suites=True),
        transform=ax.transAxes,
        ha="right",
        va="bottom",
        bbox=dict(boxstyle="round,pad=0.45", facecolor="white", edgecolor="#d1d5db", alpha=0.95),
    )

    suite_counts = Counter(point[2] for point in points)
    top_suites = [suite for suite, _ in suite_counts.most_common(20)]
    handles = [
        line_2d(
            [0],
            [0],
            marker="o",
            color="none",
            markerfacecolor=colors[suite],
            markersize=5,
            label=f"{suite} ({suite_counts[suite]})",
        )
        for suite in top_suites
    ]
    line_handles, line_labels = ax.get_legend_handles_labels()
    line_legend = ax.legend(line_handles, line_labels, loc="upper left", frameon=True, facecolor="white", edgecolor="#d1d5db")
    ax.add_artist(line_legend)
    ax.legend(
        handles=handles,
        title="Top suites by points",
        loc="center left",
        bbox_to_anchor=(1.01, 0.5),
        frameon=True,
        facecolor="white",
        edgecolor="#d1d5db",
        fontsize=8,
        title_fontsize=9,
    )
    output.parent.mkdir(parents=True, exist_ok=True)
    fig.tight_layout(rect=[0, 0, 0.80, 1])
    fig.savefig(output)


def main() -> None:
    args = parse_args()
    if args.left_report:
        neurocontainers = load_fulltest_report(args.left_report)
    elif args.neurocontainers_root:
        neurocontainers = load_neurocontainers(args.neurocontainers_root)
    else:
        raise SystemExit("provide --neurocontainers-root or --left-report")

    if args.right_report:
        tinyrange = load_fulltest_report(args.right_report)
    elif args.tinyrange_root:
        tinyrange = load_tinyrange(args.tinyrange_root)
    else:
        raise SystemExit("provide --tinyrange-root or --right-report")
    points = matched_points(neurocontainers, tinyrange)
    x_values = [point[0] for point in points]
    y_values = [point[1] for point in points]
    ratio = sum(y_values) / sum(x_values)

    plot_plain(points, args.output, args.title, args.left_label, args.right_label)
    print(args.output.resolve())

    if args.by_suite_output:
        plot_by_suite(points, args.by_suite_output, args.suite_title, args.left_label, args.right_label)
        print(args.by_suite_output.resolve())

    print(
        f"points={len(points)} suites={len({point[2] for point in points})} "
        f"ratio={ratio:.6f} overhead={(ratio - 1) * 100:.2f}%"
    )


if __name__ == "__main__":
    main()
