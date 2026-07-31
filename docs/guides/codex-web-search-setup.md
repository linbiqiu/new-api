# Codex Web Search 配置指南

本文用于配置 Codex CLI 通过 CCPG Coding Plan 使用模型和 Web Search。完成后，Codex 可以通过生产 API 调用 Responses 和实时网页搜索。

## 准备工作

你需要：

- 已安装 Codex CLI；
- 管理员或平台分配的 API 密钥，格式通常为 `sk-...`；
- 可以访问 `https://airouter.einwin.com`。

检查 Codex CLI：

```bash
codex --version
```

如果命令不存在，请先安装 Codex CLI：

```bash
npm install -g @openai/codex
```

已有 Codex CLI 的用户建议先更新：

```bash
codex update
```

## 第一步：找到配置文件

配置文件统一为 `.codex/config.toml`：

| 系统 | 路径 |
|------|------|
| macOS / Linux | `~/.codex/config.toml` |
| Windows | `%USERPROFILE%\.codex\config.toml` |

macOS / Linux：

```bash
mkdir -p ~/.codex
cp ~/.codex/config.toml ~/.codex/config.toml.bak 2>/dev/null || true
nano ~/.codex/config.toml
```

Windows PowerShell：

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\.codex" | Out-Null
if (Test-Path "$env:USERPROFILE\.codex\config.toml") {
  Copy-Item "$env:USERPROFILE\.codex\config.toml" "$env:USERPROFILE\.codex\config.toml.bak"
}
notepad "$env:USERPROFILE\.codex\config.toml"
```

> 已有配置的用户不要直接清空文件。修改同名字段，并合并 `model_providers.ccpg` 配置；TOML 不允许重复定义相同字段或表。

## 第二步：填写 Codex 配置

将下面配置合并到 `config.toml`，并把 `sk-请替换为你的密钥` 替换为平台分配的 API 密钥：

```toml
model = "gpt-5.6-sol"
model_provider = "ccpg"
web_search = "live"
experimental_bearer_token = "sk-请替换为你的密钥"

[model_providers.ccpg]
name = "OpenAI"
base_url = "https://airouter.einwin.com/v1"
wire_api = "responses"
requires_openai_auth = true
supports_websockets = false
```

关键字段说明：

| 字段 | 必须值 | 说明 |
|------|--------|------|
| `model` | `gpt-5.6-sol` | 使用支持搜索工具的模型 |
| `model_provider` | `ccpg` | 选择下方的 CCPG Provider 配置 |
| `web_search` | `live` | 启用实时 Web Search |
| `name` | `OpenAI` | Codex Provider Name；生产环境已支持其 zstd 请求压缩 |
| `base_url` | `https://airouter.einwin.com/v1` | 必须包含结尾的 `/v1` |
| `wire_api` | `responses` | Web Search 使用 Responses 协议 |

保存文件后，macOS / Linux 用户建议限制配置文件权限：

```bash
chmod 600 ~/.codex/config.toml
```

## 第三步：检查配置

运行诊断：

```bash
codex doctor --summary
```

重点确认：

- `config` 显示已加载；
- `auth` 显示已配置；
- `reachability` 显示 Provider 地址可以访问。

## 第四步：验证 Web Search

启动 Codex：

```bash
codex --search
```

然后发送：

```text
必须使用 Web Search 搜索今天的三条重要新闻。每条给出标题、来源、报道日期和可点击链接，不要依赖已有记忆回答。
```

也可以执行一次非交互测试：

```bash
codex exec --ephemeral --json --skip-git-repo-check \
  '必须调用 Web Search 查询 OpenAI Responses API 官方文档，只输出页面标题和官方 URL。'
```

成功时，JSON 输出中会先后出现：

```text
"type":"web_search"
"type":"item.completed"
```

并返回真实搜索结果。仅看到模型生成链接、没有 `web_search` 完成事件，不能视为搜索成功。

## 常见问题

### `401 Unauthorized` 或 API Key 无效

- 确认密钥完整，没有多余空格或换行；
- 确认密钥尚未过期或被禁用；
- 不要使用账号登录密码代替 API 密钥。

### `404 Not Found`

确认地址完整且只包含一个 `/v1`：

```toml
base_url = "https://airouter.einwin.com/v1"
```

### `invalid JSON request body`

Provider Name 为 `OpenAI` 时，Codex 可能发送 zstd 压缩请求。生产环境已经支持该格式；如果仍出现此错误，请把错误时间和 Request ID 提交给管理员。

### `channel does not support /v1/alpha/search`

这是服务端上游渠道配置错误，不是用户本地密钥或提示词问题。请把完整错误和 Request ID 提交给管理员，不要反复修改本地 Provider Name。

### 模型回答了，但没有真正搜索

依次检查：

1. `web_search = "live"` 是否位于顶层，而不是写在 `[model_providers.ccpg]` 内；
2. 是否使用 `gpt-5.6-sol`；
3. 是否重启了已经打开的 Codex 会话；
4. 使用 `codex --search` 启动新会话后重试；
5. 提示词中明确写出“必须使用 Web Search”。

### 修改配置后没有生效

- 完全退出并重新启动 Codex；
- 检查是否存在重复的 `model`、`model_provider` 或 `[model_providers.ccpg]`；
- 运行 `codex doctor --summary` 检查配置加载结果。

## 安全提醒

- API 密钥等同于账号调用权限，不要发送到群聊、工单截图或公开仓库；
- 分享配置时必须把密钥替换为 `<已隐藏>`；
- 怀疑密钥泄露时，立即在平台禁用旧密钥并创建新密钥；
- 不要把包含真实密钥的 `config.toml` 提交到 Git。

## 管理员确认项

如果多名用户同时无法使用 Web Search，管理员应检查：

- 生产 `/api/status` 版本是否为 `1.2.19` 或更高；
- 对应上游渠道类型是否为 `New API`；
- 上游是否支持 `/v1/responses` 和 `/v1/alpha/search`；
- 请求日志中是否存在可用于定位的 Request ID。
