#!/usr/bin/env bash
# One-time setup on a rented GPU box (Ubuntu image with CUDA
# drivers, e.g. RunPod's or Vast.ai's PyTorch template).
set -euo pipefail
cd "$(dirname "$0")"

nvidia-smi --query-gpu=name,memory.total,driver_version --format=csv

# Keep model downloads on the big volume when there is one.
if [ -d /workspace ]; then
  export HF_HOME=/workspace/hf
  grep -q HF_HOME ~/.bashrc 2>/dev/null || echo "export HF_HOME=/workspace/hf" >> ~/.bashrc
fi

command -v uv >/dev/null || curl -LsSf https://astral.sh/uv/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"

# A venv is tens of thousands of small files; on a network volume
# that is slow, so VENV can put it on local disk (.venv links to it).
VENV="${VENV:-.venv}"
uv venv --clear --python 3.12 "$VENV"
if [ "$VENV" != .venv ]; then
  ln -sfn "$VENV" .venv
fi

uv pip install -p .venv -r requirements.txt

.venv/bin/python -c "import torch, vllm, trl; print('torch', torch.__version__, 'cuda', torch.cuda.is_available(), 'vllm', vllm.__version__, 'trl', trl.__version__)"
