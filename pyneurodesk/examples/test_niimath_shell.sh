#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

tmp_bin="$(mktemp -d)"
workdir="$(mktemp -d)"
activation_script="$(mktemp)"
cleanup() {
  neurodesk_deactivate >/dev/null 2>&1 || true
  rm -rf "${tmp_bin}" "${workdir}"
  rm -f "${activation_script}"
}
trap cleanup EXIT

cat > "${tmp_bin}/neurodesk" <<EOF
#!/usr/bin/env bash
set -euo pipefail
exec uv run --project "${repo_root}" neurodesk "\$@"
EOF
chmod +x "${tmp_bin}/neurodesk"
export PATH="${tmp_bin}:${PATH}"

cd "${workdir}"
printf 'host-visible\n' > host-marker.txt

neurodesk activate --shell bash > "${activation_script}"
source "${activation_script}"
nd load niimath

niimath -help > niimath-help.txt
grep -qi "niimath" niimath-help.txt

nd exec niimath -- sh -lc 'test -f host-marker.txt && printf guest-visible > guest-output.txt && pwd > guest-pwd.txt'

test -f guest-output.txt
test "$(cat guest-output.txt)" = "guest-visible"
guest_pwd="$(cat guest-pwd.txt)"
case "${guest_pwd}" in
  /.hostcwd/*) ;;
  *)
    printf 'unexpected guest cwd: %s\n' "${guest_pwd}" >&2
    exit 1
    ;;
esac

printf 'niimath shell integration checks passed in %s\n' "${workdir}"
