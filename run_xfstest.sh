#!/bin/bash
set -e

# The debug file should be used with ./tools/build.go -dbg-tool --
DEBUG_FILE="local/xfstests.debug"

usage() {
    cat <<EOF
Usage:
  ./run_xfstest.sh [cc args...]

Special modes:
  ./run_xfstest.sh collect-artifacts <test>...
    Runs each test as generic/<test> and extracts full failure artifacts to:
      local/xfstests-artifacts/<timestamp>/
        generic/<test>.out.bad
        generic/<test>.full
        seqres.full.<test>.snippet   (if seqres.full exists)

Examples:
  ./run_xfstest.sh generic/131
  ./run_xfstest.sh shell 'ls -la /opt/xfstests'
  ./run_xfstest.sh collect-artifacts 001 023 131 478 571
EOF
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
    usage
    exit 0
fi

if [ "${1:-}" = "collect-artifacts" ]; then
    shift
    if [ "$#" -lt 1 ]; then
        echo "collect-artifacts: expected at least one test number (e.g. 131)" >&2
        exit 2
    fi

    ts="$(date -u +%Y%m%d_%H%M%S)"
    out_dir="local/xfstests-artifacts/${ts}"
    log_file="${out_dir}/xfstests.collect.log"
    tar_file="${out_dir}/xfstests.artifacts.tar"

    mkdir -p "${out_dir}"

    # Build a bash script that runs the tests and then streams back artifacts as a tarball.
    tests=("$@")
    tests_joined=""
    for t in "${tests[@]}"; do
        if [[ ! "${t}" =~ ^[0-9]{3}$ ]]; then
            echo "collect-artifacts: invalid test ${t} (expected 3 digits, e.g. 131)" >&2
            exit 2
        fi
        tests_joined+="${t} "
    done

    guest_script=$(cat <<EOF
set -euo pipefail
cd /opt/xfstests

fail_any=0
rm -rf /tmp/cc-artifacts
mkdir -p /tmp/cc-artifacts/generic

run_one() {
    t="\$1"
    name="generic/\$t"

    echo "=== CC_XFSTEST_BEGIN \${name} ==="
    if ./check "\${name}"; then
        echo "=== CC_XFSTEST_PASS \${name} ==="
        return 0
    fi
    rc="\$?"
    echo "=== CC_XFSTEST_FAIL \${name} rc=\${rc} ==="
    fail_any=1

    results_dir="/opt/xfstests/results"
    # xfstests writes these when there's an output mismatch or failure.
    if [ -f "\${results_dir}/\${name}.out.bad" ]; then
        cp -f "\${results_dir}/\${name}.out.bad" "/tmp/cc-artifacts/\${name}.out.bad"
    fi
    if [ -f "\${results_dir}/\${name}.full" ]; then
        cp -f "\${results_dir}/\${name}.full" "/tmp/cc-artifacts/\${name}.full"
    fi

    # If seqres.full exists, grab a snippet around the first matching line (or tail as fallback).
    if [ -f "\${results_dir}/seqres.full" ]; then
        snip="/tmp/cc-artifacts/seqres.full.\${t}.snippet"
        if grep -n -m1 -E "(^|[^0-9])\${t}([^0-9]|\$)|\${name}" "\${results_dir}/seqres.full" >/tmp/cc-seqres-hit.\${t}.txt 2>/dev/null; then
            line=\$(cut -d: -f1 </tmp/cc-seqres-hit.\${t}.txt || true)
            if [ -n "\${line}" ]; then
                start=\$((line - 120)); if [ "\${start}" -lt 1 ]; then start=1; fi
                end=\$((line + 240))
                sed -n "\${start},\${end}p" "\${results_dir}/seqres.full" >"\${snip}" || true
            else
                tail -n 400 "\${results_dir}/seqres.full" >"\${snip}" || true
            fi
        else
            tail -n 400 "\${results_dir}/seqres.full" >"\${snip}" || true
        fi
    fi

    return "\${rc}"
}

for t in ${tests_joined}; do
    run_one "\${t}" || true
done

echo "CC_ARTIFACTS_BEGIN"
tar -C /tmp -cf - cc-artifacts 2>/dev/null | base64 -w0 || true
echo
echo "CC_ARTIFACTS_END"

exit "\${fail_any}"
EOF
)

    set +e
    # Note: we tee so you still see progress live, while preserving the raw output for extraction.
    ./tools/build.go -run -- \
        -debug-file "${DEBUG_FILE}" \
        -add-virtiofs testfs,scratchfs \
        ./build/test-xfstests.tar shell "${guest_script}" 2>&1 | tee "${log_file}"
    run_rc="${PIPESTATUS[0]}"
    set -e

    # Extract the base64 tar region and unpack it.
    tmp_tar="${tar_file}.tmp"
    rm -f "${tmp_tar}"
    if tr -d '\r' < "${log_file}" | awk '
        $0 ~ /^CC_ARTIFACTS_BEGIN$/ {in_art=1; next}
        $0 ~ /^CC_ARTIFACTS_END$/ {in_art=0}
        in_art {print}
    ' | base64 -d > "${tmp_tar}" 2>/dev/null; then
        if [ -s "${tmp_tar}" ]; then
            mv -f "${tmp_tar}" "${tar_file}"
            tar -C "${out_dir}" -xf "${tar_file}"

            # Convenience: also place files at the expected layout directly under out_dir/.
            # The tar contains a top-level "cc-artifacts/" directory.
            if [ -d "${out_dir}/cc-artifacts" ]; then
                if [ -d "${out_dir}/cc-artifacts/generic" ]; then
                    mkdir -p "${out_dir}/generic"
                    cp -af "${out_dir}/cc-artifacts/generic/." "${out_dir}/generic/" 2>/dev/null || true
                fi
                # Copy any seqres snippets if they exist.
                shopt -s nullglob
                for f in "${out_dir}"/cc-artifacts/seqres.full.*.snippet; do
                    cp -af "${f}" "${out_dir}/" 2>/dev/null || true
                done
                shopt -u nullglob
            fi
        else
            rm -f "${tmp_tar}"
            echo "warning: artifact stream decoded but was empty (no tar written)" >&2
        fi
    else
        rm -f "${tmp_tar}"
        echo "warning: failed to decode artifact stream from log (no tar written)" >&2
    fi

    echo
    echo "Artifacts written under: ${out_dir}"
    echo "Log saved to: ${log_file}"
    exit "${run_rc}"
fi

./tools/build.go -run -- \
    -debug-file $DEBUG_FILE \
    -add-virtiofs testfs,scratchfs \
    ./build/test-xfstests.tar $@