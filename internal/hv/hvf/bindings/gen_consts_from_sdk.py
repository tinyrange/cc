#!/usr/bin/env python3
"""
Generate Go constants for Hypervisor.framework (arm64) from the local macOS SDK headers.

This is a developer convenience tool; the generated Go file is committed so builds
do NOT depend on the SDK being present at compile time.

Usage (from repo root):
  python3 internal/hv/hvf/bindings/gen_consts_from_sdk.py
"""

from __future__ import annotations

import re
from pathlib import Path


SDK = Path("/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk")
HYP_HEADERS = SDK / "System/Library/Frameworks/Hypervisor.framework/Versions/Current/Headers"
ARM64_HV = SDK / "usr/include/arm64/hv"

OUT = Path(__file__).resolve().parent / "consts_generated_darwin_arm64.go"

ENUM_MAP = {
    "hv_exit_reason": "ExitReason",
    "hv_reg": "Reg",
    "hv_simd_fp_reg": "SIMDReg",
    "hv_sme_z_reg": "SMEZReg",
    "hv_sme_p_reg": "SMEPReg",
    "hv_sys_reg": "SysReg",
    "hv_interrupt_type": "InterruptType",
    "hv_cache_type": "CacheType",
    "hv_feature_reg": "FeatureReg",
    "hv_gic_intid": "GICIntID",
    "hv_gic_distributor_reg": "GICDistributorReg",
    "hv_gic_redistributor_reg": "GICRedistributorReg",
    "hv_gic_icc_reg": "GICICCReg",
    "hv_gic_ich_reg": "GICICHReg",
    "hv_gic_icv_reg": "GICICVReg",
    "hv_gic_msi_reg": "GICMSIReg",
}


def read_text(p: Path) -> str:
    return p.read_text(encoding="utf-8", errors="replace")


def strip_comments(s: str) -> str:
    s = re.sub(r"/\*.*?\*/", "", s, flags=re.S)
    s = re.sub(r"//.*?$", "", s, flags=re.M)
    return s


def parse_os_enum_blocks(src: str) -> list[tuple[str, str]]:
    """
    Returns list of (enum_name, body_text) for OS_ENUM(enum_name, ..., <body>);
    """
    blocks: list[tuple[str, str]] = []
    pat = re.compile(r"OS_ENUM\s*\(\s*([a-zA-Z0-9_]+)\s*,\s*([a-zA-Z0-9_]+)\s*,", re.M)
    i = 0
    while True:
        m = pat.search(src, i)
        if not m:
            break
        enum_name = m.group(1)
        # Capture until the matching closing ')' of this OS_ENUM(...) invocation.
        # Some OS_ENUM blocks have trailing availability attributes after the closing ')',
        # e.g. `) API_AVAILABLE(...) API_UNAVAILABLE(...);` so we cannot simply search for `);`.
        start = m.end()
        open_idx = src.find("(", m.start())
        if open_idx == -1:
            raise RuntimeError(f"cannot find '(' for OS_ENUM({enum_name})")
        depth = 0
        close_idx = -1
        for j in range(open_idx, len(src)):
            ch = src[j]
            if ch == "(":
                depth += 1
            elif ch == ")":
                depth -= 1
                if depth == 0:
                    close_idx = j
                    break
        if close_idx == -1:
            raise RuntimeError(f"unterminated OS_ENUM({enum_name})")
        body = src[start:close_idx]
        blocks.append((enum_name, body))
        i = close_idx + 1
    return blocks


def clean_enum_body(body: str) -> str:
    # Remove doc comments and availability attributes that can appear inside the body.
    body = strip_comments(body)
    # Handle nested parentheses like API_AVAILABLE(macos(15.2)).
    body = re.sub(r"\bAPI_AVAILABLE\s*\((?:[^()]|\([^()]*\))*\)", "", body)
    body = re.sub(r"\bAPI_UNAVAILABLE\s*\((?:[^()]|\([^()]*\))*\)", "", body)
    body = re.sub(r"\bOS_ENUM\b.*?$", "", body, flags=re.M)
    # Some OS_ENUM(...) blocks have trailing availability attributes after the closing ')'.
    # After stripping API_AVAILABLE/UNAVAILABLE, remove any leftover standalone ')' lines.
    body = re.sub(r"^\s*\)\s*$", "", body, flags=re.M)
    return body


def normalize_expr(expr: str) -> str:
    expr = expr.strip()
    expr = re.sub(r"\bUINT64_C\s*\(\s*([0-9]+)\s*\)", r"\1", expr)
    expr = re.sub(r"\bUINT32_C\s*\(\s*([0-9]+)\s*\)", r"\1", expr)
    expr = expr.replace("ull", "").replace("ULL", "").replace("ul", "").replace("UL", "")
    return expr.strip()


def try_parse_int(expr: str) -> int | None:
    expr = normalize_expr(expr)
    if re.fullmatch(r"0x[0-9a-fA-F]+", expr):
        return int(expr, 16)
    if re.fullmatch(r"[0-9]+", expr):
        return int(expr, 10)
    m = re.fullmatch(r"\(\s*1\s*<<\s*([0-9]+)\s*\)", expr)
    if m:
        return 1 << int(m.group(1), 10)
    m = re.fullmatch(r"\(\s*1\s*<<\s*([0-9]+)\s*\)", expr)
    if m:
        return 1 << int(m.group(1), 10)
    m = re.fullmatch(r"\(\s*1\s*<<\s*([0-9]+)\s*\)", expr)
    if m:
        return 1 << int(m.group(1), 10)
    m = re.fullmatch(r"\(\s*1\s*<<\s*([0-9]+)\s*\)", expr)
    if m:
        return 1 << int(m.group(1), 10)
    m = re.fullmatch(r"\(\s*1\s*<<\s*([0-9]+)\s*\)", expr)
    if m:
        return 1 << int(m.group(1), 10)
    m = re.fullmatch(r"\(\s*1\s*<<\s*([0-9]+)\s*\)", expr)
    if m:
        return 1 << int(m.group(1), 10)
    m = re.fullmatch(r"\(\s*1\s*<<\s*([0-9]+)\s*\)", expr)
    if m:
        return 1 << int(m.group(1), 10)
    # Common form in headers: (1 << n)
    m = re.fullmatch(r"\(\s*1\s*<<\s*([0-9]+)\s*\)", expr)
    if m:
        return 1 << int(m.group(1), 10)
    return None


def parse_enum_items(body: str) -> list[tuple[str, str | None]]:
    body = clean_enum_body(body)
    items: list[tuple[str, str | None]] = []
    for raw in body.split(","):
        t = raw.strip()
        if not t:
            continue
        if "=" in t:
            name, expr = t.split("=", 1)
            items.append((name.strip(), expr.strip()))
        else:
            items.append((t, None))
    return items


def generate_go_consts_for_os_enum(enum_name: str, body: str) -> str | None:
    go_type = ENUM_MAP.get(enum_name)
    if go_type is None:
        return None

    items = parse_enum_items(body)
    # Track numeric values where possible to assign explicit numbers for implicit entries.
    known: dict[str, int] = {}
    cur: int | None = None
    lines: list[str] = []

    for name, expr in items:
        if expr is None:
            if cur is None:
                cur = 0
            else:
                cur += 1
            known[name] = cur
            lines.append(f"\t{name} {go_type} = {cur}")
            continue

        expr_n = normalize_expr(expr)
        # Alias?
        if re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", expr_n) and expr_n in known:
            alias_target = expr_n
            cur = known[alias_target]
            known[name] = cur
            lines.append(f"\t{name} {go_type} = {alias_target}")
            continue

        v = try_parse_int(expr_n)
        if v is not None:
            cur = v
            known[name] = v
            # Prefer hex for big values.
            if v >= 16:
                lines.append(f"\t{name} {go_type} = 0x{v:x}")
            else:
                lines.append(f"\t{name} {go_type} = {v}")
            continue

        # Fallback: emit expression verbatim (rare on arm64 headers).
        cur = None
        lines.append(f"\t{name} {go_type} = {expr_n}")

    if not lines:
        return None

    return "const (\n" + "\n".join(lines) + "\n)\n"


def parse_hv_kern_types() -> str:
    """
    hv_kern_types.h contains hv_return_t codes and HV_MEMORY_* flags as enums.
    The error codes use Mach macros, so we use the numeric values in comments.
    """
    src = read_text(ARM64_HV / "hv_kern_types.h")
    out_lines: list[str] = []

    # Return codes
    return_codes: list[tuple[str, str]] = []
    for line in src.splitlines():
        line = line.strip()
        m = re.match(r"^(HV_[A-Z0-9_]+)\s*=\s*[^,]+,?\s*/\*\s*\((0x[0-9a-fA-F]+)\)\s*\*/", line)
        if m:
            return_codes.append((m.group(1), m.group(2)))
        m2 = re.match(r"^(HV_SUCCESS)\s*=\s*([0-9]+)\s*,?$", line)
        if m2:
            return_codes.append((m2.group(1), m2.group(2)))

    # Ensure HV_ILLEGAL_GUEST_STATE is included (it has a comment too).
    def to_int32_literal(val: str) -> str:
        val = val.strip()
        if val.startswith("0x") or val.startswith("0X"):
            v = int(val, 16)
            if v > 0x7FFFFFFF:
                v = v - 0x100000000
            # Prefer hex formatting for readability.
            if v < 0:
                return f"-0x{(-v):x}"
            return f"0x{v:x}"
        v = int(val, 10)
        if v > 0x7FFFFFFF:
            v = v - 0x100000000
        return str(v)

    if return_codes:
        out_lines.append("const (")
        for name, val in return_codes:
            out_lines.append(f"\t{name} Return = {to_int32_literal(val)}")
        out_lines.append(")\n")

    # Memory flags: parse (1ull<<n)
    mem_flags: list[tuple[str, int]] = []
    for line in src.splitlines():
        line = line.strip()
        m = re.match(r"^(HV_MEMORY_[A-Z0-9_]+)\s*=\s*\(\s*1ull\s*<<\s*([0-9]+)\s*\)\s*,?$", line)
        if m:
            mem_flags.append((m.group(1), 1 << int(m.group(2), 10)))

    if mem_flags:
        out_lines.append("const (")
        for name, v in mem_flags:
            out_lines.append(f"\t{name} MemoryFlags = 0x{v:x}")
        out_lines.append(")\n")

    return "\n".join(out_lines)


def parse_hv_vm_allocate_flags() -> str:
    src = read_text(HYP_HEADERS / "hv_vm_allocate.h")
    src_nc = strip_comments(src)
    # Find enum { ... HV_ALLOCATE_DEFAULT ... };
    m = re.search(r"enum\s*\{([^}]*)\}\s*;\s*typedef\s+uint64_t\s+hv_allocate_flags_t\s*;", src_nc, flags=re.S)
    if not m:
        # Fallback: simple direct scan.
        m = re.search(r"enum\s*\{([^}]*)\}\s*;", src_nc, flags=re.S)
        if not m:
            return ""
    body = m.group(1)
    items = []
    for raw in body.split(","):
        t = raw.strip()
        if not t:
            continue
        if "=" in t:
            name, expr = t.split("=", 1)
            items.append((name.strip(), normalize_expr(expr)))
    lines = []
    for name, expr in items:
        if name == "HV_ALLOCATE_DEFAULT":
            v = try_parse_int(expr)
            if v is None:
                v = 0
            lines.append(f"\t{name} AllocateFlags = {v}")
    if not lines:
        return ""
    return "const (\n" + "\n".join(lines) + "\n)\n"


def main() -> None:
    parts: list[str] = []
    parts.append("// Code generated by internal/hv/hvf/bindings/gen_consts_from_sdk.py; DO NOT EDIT.")
    parts.append("//go:build darwin && arm64")
    parts.append("")
    parts.append("package bindings")
    parts.append("")

    # hv_return_t + HV_MEMORY_* from the arm64 kernel header.
    parts.append(parse_hv_kern_types())
    parts.append(parse_hv_vm_allocate_flags())

    # OS_ENUM blocks from framework headers.
    sources = [
        HYP_HEADERS / "hv_vcpu_types.h",
        HYP_HEADERS / "hv_vcpu_config.h",
        HYP_HEADERS / "hv_gic_types.h",
    ]
    for p in sources:
        src = read_text(p)
        for enum_name, body in parse_os_enum_blocks(src):
            chunk = generate_go_consts_for_os_enum(enum_name, body)
            if chunk:
                parts.append(chunk)

    OUT.write_text("\n".join([p for p in parts if p is not None]), encoding="utf-8")


if __name__ == "__main__":
    main()


