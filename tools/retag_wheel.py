#!/usr/bin/env python3
"""Retag a pure-Python wheel after adding a platform-specific bundled binary."""

from __future__ import annotations

import argparse
import base64
import csv
import hashlib
import io
import shutil
import tempfile
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo


def main() -> None:
    parser = argparse.ArgumentParser(description="Replace a wheel's platform tag")
    parser.add_argument("wheel", type=Path)
    parser.add_argument("--platform-tag", required=True)
    args = parser.parse_args()

    wheel = args.wheel
    parts = wheel.name.removesuffix(".whl").split("-")
    if len(parts) != 5:
        raise SystemExit(f"unsupported wheel filename: {wheel.name}")

    distribution, version, python_tag, abi_tag, _platform_tag = parts
    output = wheel.with_name(
        f"{distribution}-{version}-{python_tag}-{abi_tag}-{args.platform_tag}.whl"
    )

    with tempfile.TemporaryDirectory() as tmp:
        tmp_output = Path(tmp) / output.name
        with ZipFile(wheel, "r") as src:
            record_name = next(
                name for name in src.namelist() if name.endswith(".dist-info/RECORD")
            )
            entries: list[tuple[ZipInfo, bytes]] = []
            for info in src.infolist():
                if info.filename == record_name:
                    continue
                data = src.read(info.filename)
                if info.filename.endswith(".dist-info/WHEEL"):
                    text = data.decode("utf-8")
                    lines = []
                    for line in text.splitlines():
                        if line.startswith("Tag: "):
                            lines.append(f"Tag: {python_tag}-{abi_tag}-{args.platform_tag}")
                        else:
                            lines.append(line)
                    data = ("\n".join(lines) + "\n").encode("utf-8")
                entries.append((copy_info(info), data))

        records = [record_for(info.filename, data) for info, data in entries]
        records.append((record_name, "", ""))
        record_info = ZipInfo(record_name)
        record_info.compress_type = ZIP_DEFLATED
        entries.append((record_info, render_record(records)))

        with ZipFile(tmp_output, "w", ZIP_DEFLATED) as dst:
            for info, data in entries:
                dst.writestr(info, data)

        validate_record(tmp_output)
        shutil.move(str(tmp_output), output)

    if output != wheel:
        wheel.unlink()
    print(output)


def copy_info(info: ZipInfo) -> ZipInfo:
    copied = ZipInfo(info.filename, date_time=info.date_time)
    copied.compress_type = ZIP_DEFLATED
    copied.external_attr = info.external_attr
    copied.comment = info.comment
    copied.extra = info.extra
    copied.internal_attr = info.internal_attr
    return copied


def record_for(name: str, data: bytes) -> tuple[str, str, str]:
    digest = hashlib.sha256(data).digest()
    encoded = base64.urlsafe_b64encode(digest).rstrip(b"=").decode("ascii")
    return name, f"sha256={encoded}", str(len(data))


def render_record(rows: list[tuple[str, str, str]]) -> bytes:
    output = io.StringIO()
    writer = csv.writer(output, lineterminator="\n")
    writer.writerows(rows)
    return output.getvalue().encode("utf-8")


def validate_record(wheel: Path) -> None:
    with ZipFile(wheel) as zf:
        names = zf.namelist()
        if len(names) != len(set(names)):
            raise SystemExit(f"duplicate filenames in wheel: {wheel}")
        record_name = next(name for name in names if name.endswith(".dist-info/RECORD"))
        rows = list(csv.reader(io.StringIO(zf.read(record_name).decode("utf-8"))))
        recorded = {row[0] for row in rows}
        if set(names) != recorded:
            missing = sorted(set(names) - recorded)
            extra = sorted(recorded - set(names))
            raise SystemExit(f"RECORD entries do not match wheel contents: missing={missing}, extra={extra}")
        for path, digest, size in rows:
            if path == record_name:
                if digest or size:
                    raise SystemExit(f"RECORD self-entry must not include hash or size: {wheel}")
                continue
            data = zf.read(path)
            if size and int(size) != len(data):
                raise SystemExit(f"RECORD size mismatch for {path}: {size} != {len(data)}")
            if digest:
                algorithm, expected = digest.split("=", 1)
                actual = base64.urlsafe_b64encode(
                    hashlib.new(algorithm, data).digest()
                ).rstrip(b"=").decode("ascii")
                if actual != expected:
                    raise SystemExit(f"RECORD digest mismatch for {path}")


if __name__ == "__main__":
    main()
