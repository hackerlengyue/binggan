#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { printf '结构检查失败：%s\n' "$*" >&2; exit 1; }

[[ -f go.mod && -f mygo.config.ts && -f frontend/package.json ]] || fail '缺少唯一根配置'
[[ ! -d backend && ! -d src ]] || fail '存在旧业务树或脚手架前端'
[[ $(find . -path './.git' -prune -o -path './node_modules' -prune -o -path './frontend/node_modules' -prune -o -name go.mod -print | wc -l | tr -d ' ') == 1 ]] || fail 'Go module 数量不是 1'
[[ $(find . -path './.git' -prune -o -path './node_modules' -prune -o -path './frontend/node_modules' -prune -o -name vite.config.ts -print | wc -l | tr -d ' ') == 1 ]] || fail 'Vite 工程数量不是 1'
if git rev-parse --git-dir >/dev/null 2>&1; then
  if git ls-files | grep -E '(^|/)(node_modules|dist|build|data)/|\.(db|sqlite|log)$|\.key$' >/dev/null; then
    fail '跟踪了构建产物、依赖或用户数据'
  fi
fi

config_version=$(sed -n 's/^ *version: "\([^"]*\)".*/\1/p' mygo.config.ts)
frontend_version=$(sed -n 's/^ *"version": "\([^"]*\)".*/\1/p' frontend/package.json)
[[ -n $config_version && $config_version == "$frontend_version" ]] || fail "mygo.config.ts 与 frontend/package.json 版本不一致（$config_version / $frontend_version）"

if [[ ${1:-} == --final ]]; then
  if git grep -nE 'gin-gonic|github.com/wailsapp|EventSource|bridgeJS' -- . ':!*.md' ':!go.sum' ':!package-lock.json' ':!scripts/check-structure.sh'; then
    fail '仍有旧框架或 SSE/桥实现'
  fi
  if git grep -nE 'fetch\(|new XMLHttpRequest\(|new EventSource\(|apiURL\(' -- frontend/src; then
    fail '仍有普通业务 HTTP 调用'
  fi
fi
printf '结构检查通过%s\n' "${1:+（最终模式）}"
