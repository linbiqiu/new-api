# Codex Web Search 配置

将 Codex Provider Name 设置为 `OpenAI`，并启用实时 Web Search。

## 1. 打开配置文件

- macOS / Linux：`~/.codex/config.toml`
- Windows：`%USERPROFILE%\.codex\config.toml`

## 2. 修改配置

先找到当前使用的 Provider：

```toml
model_provider = "custom"
```

然后找到对应的 `[model_providers.custom]`，将 `name` 改为 `OpenAI`：

```toml
model_provider = "custom"
web_search = "live"

[model_providers.custom]
name = "OpenAI"
wire_api = "responses"
requires_openai_auth = true
base_url = "https://airouter.einwin.com/v1"
```

如果你的 `model_provider` 不是 `custom`，配置段名称必须保持一致。例如：

```toml
model_provider = "ccpg"
web_search = "live"

[model_providers.ccpg]
name = "OpenAI"
```

注意：

- 只修改当前 Provider 的 `name`，不要新增另一个无关 Provider；
- `web_search = "live"` 必须写在顶层，不能写进 `[model_providers.xxx]`；
- 保留原来的 API 地址、密钥、模型及其他配置。

## 3. 重启并验证

完全退出并重新启动 Codex，然后发送：

```text
必须使用 Web Search 搜索今天的三条重要新闻，并给出来源和链接。
```

也可以使用以下命令启动：

```bash
codex --search
```

能够返回实时新闻、来源和有效链接，即表示 Web Search 配置成功。
