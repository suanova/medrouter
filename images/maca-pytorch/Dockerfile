# Built with Docker 29's builtin Dockerfile frontend: the `# syntax=` pull of
# docker.io/docker/dockerfile does not go through the daemon proxy here and
# fails with EOF. The builtin frontend supports the heredocs used below.
#
# medrouter-lora — the LoRA / SFT training layer for MedRouter's one-click
# fine-tuning.
#
# The base is a framework image only: MACA runtime + PyTorch. It carries no
# Hugging Face stack, so a training job would otherwise spend minutes on
# `pip install` at start-up and depend on PyPI being reachable from the pod.
# This layer bakes the stack in so a job starts training immediately.
#
# Build (the cluster is amd64):
#   docker build --platform=linux/amd64 \
#     -t harbor.isuanova.com/suanova/medrouter-lora:maca3.8.5-py312 \
#     images/maca-pytorch
#
# See README.md for the version matrix and what is deliberately left out.

ARG BASE_IMAGE=harbor.isuanova.com/suanova/maca-pytorch:3.8.5.2-py312-torch2.8-d11231e
FROM ${BASE_IMAGE}

# The base image runs as uid 1000 and /opt is root-owned; install as root and
# drop back to the unprivileged user before the runtime stage is sealed.
USER root

LABEL org.opencontainers.image.title="medrouter-lora" \
      org.opencontainers.image.description="LoRA/SFT training stack on MetaX MACA + PyTorch" \
      org.opencontainers.image.base.name="${BASE_IMAGE}" \
      medrouter.io/maca.version="3.8.5.2" \
      medrouter.io/torch.version="2.8" \
      medrouter.io/python.version="3.12"

ENV PIP_NO_CACHE_DIR=1 \
    PYTHONUNBUFFERED=1 \
    TOKENIZERS_PARALLELISM=false \
    HF_HUB_DISABLE_TELEMETRY=1 \
    HF_HOME=/opt/huggingface

COPY requirements.txt /opt/medrouter/requirements.txt

# The training entrypoint. It is baked in rather than mounted so a job cannot
# run a different script than the image it pins.
COPY train_lora.py /opt/medrouter/train_lora.py

# Pin torch to the version already in the base image. Without this constraint a
# dependency could pull the PyPI wheel and silently replace the MACA build, and
# every later import fails at runtime instead of here.
RUN python3 - <<'PY' > /opt/medrouter/constraints.txt
import torch
print("torch==" + torch.__version__.split("+")[0])
PY

RUN python3 -m pip install --no-cache-dir \
        -c /opt/medrouter/constraints.txt \
        -r /opt/medrouter/requirements.txt \
 && python3 -m pip check

# Fail the build rather than the first training job if the stack cannot import.
# torch.cuda.is_available() is false here because the build has no device; the
# MACA runtime is exercised by the job, not by the build.
RUN python3 - <<'PY'
import torch, transformers, peft, trl, datasets, accelerate, pyarrow

print("torch        ", torch.__version__)
print("transformers ", transformers.__version__)
print("peft         ", peft.__version__)
print("trl          ", trl.__version__)
print("datasets     ", datasets.__version__)
print("accelerate   ", accelerate.__version__)
print("pyarrow      ", pyarrow.__version__)
print("maca version attrs:", [a for a in dir(torch.version) if "maca" in a.lower()] or "none")
PY

# Back to the unprivileged runtime user the base image expects.
USER 1000:1000

WORKDIR /workspace
