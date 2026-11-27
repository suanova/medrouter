#!/bin/sh
# DSH 容器入口：首次启动把镜像内置的 profile 种子复制到数据卷，准备 Agent
# 工作目录，然后以容器网卡地址:3080 启动 Web 服务（端口/网卡由 overlay.yml 固定）。
set -eu

export DSH_HOME="${DSH_HOME:-/data}"

if [ ! -f "$DSH_HOME/profiles/web/package.json" ]; then
    mkdir -p "$DSH_HOME/profiles"
    cp -a /opt/dsh-seed/profiles/. "$DSH_HOME/profiles/"
fi

# pnpm 以相对路径记录本地插件链接；数据卷深度变化后可能失效。每次启动都把
# 镜像内的本地插件重新链接过去，已有数据卷也能拿到随镜像升级的新插件。
if [ -d "$DSH_HOME/profiles/web" ]; then
    mkdir -p "$DSH_HOME/profiles/web/node_modules"
    for plugin in dsh-api medrouter-skills; do
        ln -sfn "/opt/dsh-plugins/$plugin" "$DSH_HOME/profiles/web/node_modules/$plugin"
    done
fi

export DSH_WORKSPACE="${DSH_WORKSPACE:-$DSH_HOME/workspace}"
mkdir -p "$DSH_WORKSPACE"
if [ ! -f "$DSH_WORKSPACE/AGENTS.md" ]; then
    cp /opt/dsh/workspace-AGENTS.md "$DSH_WORKSPACE/AGENTS.md"
fi

# 按用户拆分的任务目录（$DSH_WORKSPACE/<userId>）不是共享根的子孙项目，
# AGENTS.md 的项目根发现走不到共享根那份约定；DSH_HOME/AGENTS.md 是 user-global
# 指令文件，任何 cwd 都会加载。共享根那份保留给旧任务。
if [ ! -f "$DSH_HOME/AGENTS.md" ]; then
    cp /opt/dsh/workspace-AGENTS.md "$DSH_HOME/AGENTS.md"
fi

# dsh 0.2.2 起拒绝 0.0.0.0 等通配地址，只接受具体网卡的 IP 字面量。容器内取
# 第一个非回环 IPv4（即 eth0），并通过 DSH_BIND_HOST 交给 overlay.yml；同时把
# 它写到文件里，供健康检查探活。
if [ -z "${DSH_BIND_HOST:-}" ]; then
    DSH_BIND_HOST=$(node -e 'const n = require("node:os").networkInterfaces(); for (const list of Object.values(n)) for (const a of list || []) if (a.family === "IPv4" && !a.internal) { process.stdout.write(a.address); process.exit(0) } process.exit(1)') || DSH_BIND_HOST=127.0.0.1
    export DSH_BIND_HOST
fi
printf '%s\n' "$DSH_BIND_HOST" > /tmp/dsh-bind-host

# 模型目录：DSH 只服务用户自己部署的推理服务。MedRouter 提供运行中的服务列表，
# 这里为每个服务生成一条 pi-ai 的 openai-completions 路由；请求体里的 model 就是
# 服务名，私有 AI 网关据此选中后端模型（客户端不需要自己加路由头）。文件随容器
# 启动生成；服务集合变化时 MedRouter 会给 Deployment 打 annotation 触发滚动重启，
# 从而重新拉取。
MODELS_FILE="$DSH_HOME/runtime-models.yml"
export DSH_LLM_BASE_URL="${DSH_LLM_BASE_URL:-http://cubestack-ai-private.envoy-gateway-system.svc.cluster.local/v1}"
export DSH_LLM_CONTEXT_WINDOW="${DSH_LLM_CONTEXT_WINDOW:-32768}"
export DSH_LLM_MAX_TOKENS="${DSH_LLM_MAX_TOKENS:-8192}"
python3 - "$MODELS_FILE" <<'PY'
import json
import os
import sys
import time
import urllib.request

path = sys.argv[1]
url = os.environ.get("MEDROUTER_MODELS_URL", "")
key = os.environ.get("DSH_API_KEY", "")
base_url = os.environ.get("DSH_LLM_BASE_URL", "")
prefix = os.environ.get("DSH_MODEL_PROVIDER_PREFIX", "mr-")
try:
    context_window = int(os.environ.get("DSH_LLM_CONTEXT_WINDOW", "32768"))
    max_tokens = int(os.environ.get("DSH_LLM_MAX_TOKENS", "8192"))
except ValueError:
    context_window, max_tokens = 32768, 8192

def fetch_models():
    request = urllib.request.Request(
        url,
        headers={"Authorization": "Bearer " + key, "Accept": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        payload = json.load(response)
    return payload.get("data", {}).get("models", [])

models = []
if url:
    for attempt in range(30):
        try:
            models = fetch_models()
            break
        except Exception as exc:  # noqa: BLE001 - any failure means retry
            print(f"dsh: model catalog fetch failed ({attempt + 1}/30): {exc}", file=sys.stderr)
            time.sleep(2)

quote = json.dumps
lines = [
    "# Generated at container start from MedRouter's running inference services.",
    "- insert:",
    "    - id: llm-pi-ai",
    "      name: '@deepseek-ai/dsh-llm-pi-ai'",
    "      config:",
]
routes = []
for entry in models:
    name = str(entry.get("id") or "").strip()
    if not name:
        continue
    display = str(entry.get("name") or name).strip() or name
    provider = prefix + name
    routes += [
        f"          {quote(provider)}:",
        f"            displayName: {quote(display)}",
        "            api: openai-completions",
        f"            baseURL: {quote(base_url)}",
        "            apiKeyEnv: DSH_API_KEY",
        f"            defaultContextWindow: {context_window}",
        f"            defaultMaxTokens: {max_tokens}",
        "            models:",
        f"              - id: {quote(name)}",
        f"                name: {quote(display)}",
        f"                contextWindow: {context_window}",
        f"                maxTokens: {max_tokens}",
    ]
if routes:
    lines.append("        providers:")
    lines += routes
else:
    lines.append("        providers: {}")

with open(path, "w", encoding="utf-8") as handle:
    handle.write("\n".join(lines) + "\n")
print(f"dsh: wrote {len(models)} model route(s) to {path}", file=sys.stderr)
PY

# 启动器自己的选项（--patch）必须排在应用参数之前：解析器遇到第一个不认识的
# 参数后，其余全部原样交给 web 应用（--no-open 等）。第二个 patch 是刚生成的
# 模型目录。
set -- web --patch /opt/dsh/overlay.yml --patch "$MODELS_FILE" --no-open

# 通过非 loopback 主机名访问 Web UI 时，浏览器信任栅栏要求显式声明该 authority
# （空格分隔的 host 或 host:port 列表）。
# shellcheck disable=SC2086
if [ -n "${DSH_TRUSTED_HOST:-}" ]; then
    set -- "$@" --trusted-host $DSH_TRUSTED_HOST
fi

exec dsh "$@"
