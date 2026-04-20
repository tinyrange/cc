#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$script_dir"

if command -v uv >/dev/null 2>&1; then
    exec uv run --with jupyterlab jupyter lab --notebook-dir "$script_dir" "$@"
fi

if [ -x "$script_dir/.venv/bin/jupyter-lab" ]; then
    exec "$script_dir/.venv/bin/jupyter-lab" --notebook-dir "$script_dir" "$@"
fi

echo "error: neither 'uv' nor '.venv/bin/jupyter-lab' is available" >&2
echo "install uv or add jupyterlab to the local virtual environment" >&2
exit 1
