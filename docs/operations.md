# 运行与开发

本文档说明如何部署、配置、调用和开发模汇。项目介绍见 [README](../README.md)，协议契约与安全决策见 [需求与设计](requirements.md)。操作说明随实现维护：配置以 [配置定义](../internal/config/config.go) 为准，构建与发布以 [Dockerfile](../Dockerfile)、[前端脚本](../web/package.json) 和 [发布工作流](../.github/workflows/release.yml) 为准。

## 二进制与首次启动

启动命令见 [README](../README.md#下载与运行)。发布包命名如下，`VERSION` 对应去掉 `v` 前缀的发布标签；每个 Release 同时提供 `checksums.txt`。

| 平台 | 文件 |
| --- | --- |
| macOS Apple Silicon | `model-confluence_VERSION_darwin_arm64.tar.gz` |
| Windows 64 位 | `model-confluence_VERSION_windows_amd64.zip` |
| Linux 64 位 | `model-confluence_VERSION_linux_amd64.tar.gz` |

macOS 发布流程未做 Apple Developer 签名和公证，首次运行可能需要在系统安全设置中确认打开。

初始管理员密码仅在数据库尚未建立管理员时使用，应用保存密码哈希。再次启动时修改 `MODEL_CONFLUENCE_ADMIN_PASSWORD` 不会覆盖已有密码。其他访问密钥和供应商密钥按产品要求保留明文。

## Docker 与 Compose

镜像为 `ghcr.io/sanjeever/model-confluence`，支持 Linux `amd64` 和 `arm64`。镜像内以非 root 用户运行，监听 `0.0.0.0:8080`，数据目录为 `/data`。以下示例只在本机暴露端口并使用命名卷保存 SQLite：

```bash
docker run -d --name model-confluence -p 127.0.0.1:8080:8080 -v model-confluence-data:/data -e MODEL_CONFLUENCE_ADMIN_PASSWORD="请替换为管理员密码" ghcr.io/sanjeever/model-confluence:latest
```

使用 Compose 时，新建 `compose.yaml`：

```yaml
services:
  model-confluence:
    image: ghcr.io/sanjeever/model-confluence:latest
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      MODEL_CONFLUENCE_ADMIN_PASSWORD: "${MODEL_CONFLUENCE_ADMIN_PASSWORD:-}"
    volumes:
      - model-confluence-data:/data

volumes:
  model-confluence-data:
```

首次启动前，在当前终端设置管理员密码：

```bash
export MODEL_CONFLUENCE_ADMIN_PASSWORD="请替换为管理员密码"
docker compose up -d
```

Windows PowerShell：

```powershell
$env:MODEL_CONFLUENCE_ADMIN_PASSWORD = "请替换为管理员密码"
docker compose up -d
```

升级时保留数据卷，拉取新镜像并重建容器：

```bash
docker compose pull
docker compose up -d
```

二进制升级时先停止旧进程，再替换可执行文件并使用原数据目录启动。启动时自动迁移数据库；不要同时运行多个实例访问同一数据库。使用宿主机目录挂载时，需保证容器运行用户可写。

## 运行参数

下表是配置代码的使用说明；命令行参数覆盖对应环境变量，未提供时使用默认值。Go duration 参数使用 `10s`、`5m` 等形式。

| 参数 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `--listen` | `MODEL_CONFLUENCE_LISTEN` | `127.0.0.1:8080` | HTTP 监听地址；容器启动参数另行指定 |
| `--data-dir` | `MODEL_CONFLUENCE_DATA_DIR` | `data` | 数据目录；相对路径以启动工作目录为基准 |
| `--admin-password` | `MODEL_CONFLUENCE_ADMIN_PASSWORD` | 空 | 初始或重置后的管理员密码 |
| `--trusted-proxies` | `MODEL_CONFLUENCE_TRUSTED_PROXIES` | 空 | 逗号分隔的可信代理 CIDR |
| `--connect-timeout` | — | `10s` | 上游连接超时 |
| `--response-header-timeout` | — | `5m` | 等待上游响应头超时 |
| `--stream-idle-timeout` | — | `5m` | 上游响应读取空闲超时，流式与非流式生成响应均使用 |
| `--stream-heartbeat-interval` | — | `0` | SSE 心跳间隔；`0` 关闭心跳 |
| `--max-request-bytes` | — | `67108864` | 入站生成请求体上限（64 MiB），必须大于 0 |
| `--log-retention-days` | `MODEL_CONFLUENCE_LOG_RETENTION_DAYS` | `0` | 完整日志载荷保留天数；`0` 永久保留，不能为负数 |
| — | `MODEL_CONFLUENCE_ANTIGRAVITY_OAUTH_CLIENT_ID` | 空 | 签发 Antigravity refresh token 的 OAuth 客户端 ID |
| — | `MODEL_CONFLUENCE_ANTIGRAVITY_OAUTH_CLIENT_SECRET` | 空 | 上述 OAuth 客户端的 secret，只在运行时注入 |

上游生成请求不设置总时长上限。心跳只能维持客户端连接，不会重置上游读取空闲超时；开启心跳后，HTTP 响应可能在收到模型内容前已发送 `200`，后续错误只能以 SSE 表达。

设置保留天数后，服务启动时及后台定期清理已结束请求的过期正文、头部和错误详情，保留请求与 attempt 元数据及 usage，并标记载荷已清理；过期精简安全事件会删除。该选项不会删除配置中的访问密钥或供应商密钥，也不提供按时间范围手工删除使用记录。

`GET /healthz` 无需鉴权，仅检查 SQLite 可访问，不探测上游连通性。定义见 [HTTP 装配](../internal/app/app.go)。

## 管理后台与客户端调用

登录管理后台后，创建访问密钥。普通供应商配置完整协议端点和上游密钥；OAuth 供应商按下文导入账号凭据。随后创建虚拟模型及有序候选。客户端的 `model` 使用虚拟模型名。

获取模型列表：

```powershell
curl.exe http://127.0.0.1:8080/v1/models `
  -H "Authorization: Bearer mc_your_access_key"
```

调用 Chat Completions（PowerShell 7）：

```powershell
curl.exe http://127.0.0.1:8080/v1/chat/completions `
  -H "Authorization: Bearer mc_your_access_key" `
  -H "Content-Type: application/json" `
  -d '{"model":"your-virtual-model","messages":[{"role":"user","content":"你好"}],"stream":false}'
```

模型 API 接受 `Authorization: Bearer <key>` 或 `x-api-key: <key>`；同时提供且值不一致时拒绝。入口与鉴权定义见 [网关](../internal/gateway/handler.go)。模型列表表示管理员启用的模型，不保证每个模型此刻都有可用路由。

配置候选时可以手动拉取供应商模型名；普通供应商从生成端点推导列表地址，Antigravity 使用专属元数据接口，Codex 不支持此功能。模型目录不表示账号具有相应调用权限，也不会自动同步路由。管理后台的连通性测试针对虚拟模型，经过实际路由选择、转换、密钥池及日志流程；一次成功只说明本次选中的路由可用，不验证所有候选、协议或流式能力。

### 浏览器登录

在 OAuth 供应商的账号列表点击「登录添加账号」，在弹窗中打开授权页，在 OpenAI 或 Google 页面完成登录。模汇通过临时回环 HTTP 服务接收回调，在后端交换令牌并写入现有账号池；后台轮询结果，成功后关闭弹窗并刷新账号列表。密码不经过模汇，浏览器和管理 API 不接收上游令牌。

远程或 Docker 部署可以直接回填完整回调 URL，无需 SSH 隧道：保持模汇的登录弹窗打开，在供应商页面授权后，如果浏览器提示无法连接 `127.0.0.1`，复制地址栏的完整 URL，返回模汇粘贴到「完整回调 URL」，点击「提交回调」。后端解析授权结果，校验登录归属、回调地址、`state` 和提供的 issuer，再使用原登录会话保存的 PKCE verifier 交换令牌；不会访问提交的 URL。成功、失败和取消继续使用同一套状态查询。回调只可使用一次，过期、已取消或其他登录生成的地址不能提交；不要提交此前已成功使用的旧地址。

| 供应商 | 临时回调地址 |
| --- | --- |
| Codex OAuth | `http://127.0.0.1:1455/auth/callback` |
| Antigravity OAuth | `http://127.0.0.1:51121/oauth-callback` |

登录使用授权码与 PKCE S256，`state` 一次性校验。每个管理员会话同时允许一个待完成登录，5 分钟过期；取消、超时、成功和失败后释放监听端口及临时凭据。退出管理后台、修改管理员密码或关闭登录组件会取消待完成登录。登录过程只保存在内存，进程重启后重新发起。端口被其他程序占用时明确报错；Codex CLI 或其他实例可能占用 1455。

本机浏览器与服务进程处于同一网络环境时，可直接自动回调。远程浏览器中的 `127.0.0.1` 指向用户电脑；若希望远程自动回调，也可先建立 SSH 隧道，并保持连接，再从远程后台发起登录：

```bash
ssh -N -L 1455:127.0.0.1:1455 -L 51121:127.0.0.1:51121 user@server
```

回调服务始终绑定进程所在网络命名空间的回环地址。普通 Docker `-p` 映射不能访问容器回环；Docker 桥接部署建议使用回调 URL 回填。Linux 主机网络部署可按上例转发，其他网络环境使用自动回调时需在同一网络命名空间建立隧道。本版本不提供公网自动回调或设备码登录。

Antigravity 登录仍需配置下文的 OAuth 客户端，并确认它允许表中的回调地址及所需 scope；授权请求包含 `openid`、`userinfo.email` 和 `cloud-platform`，要求离线访问。交换令牌后还会验证 Google 身份和查询项目；缺少 refresh token 或项目时明确失败。配置客户端不代表拥有订阅模型权限，尚无真实 AGY 订阅账号验收。

登录只添加账号，不覆盖已有账号令牌。遇到重复账号时明确提示；永久鉴权失效时删除旧账号，再登录或导入。供应商仍保持管理员设置的启停状态，登录不会开启已停用的供应商。完整回调 URL、授权码、PKCE verifier、令牌交换响应不进入模型载荷或普通运行日志。手动提交通过有管理员会话及 CSRF 校验的管理 POST 接口处理，输入只在当前页面内存中保留，处理结束后清空。

### Antigravity OAuth

此接入为实验性能力，尚无真实订阅账号验收。Google 当前[服务条款第 6 条](https://antigravity.google/terms)限制个人订阅通过第三方工具访问，并说明可能暂停或终止 Antigravity、Gemini CLI 账号。接通内部接口不代表官方允许将订阅用于第三方 API 网关。

1. 在运行环境注入上述 OAuth client ID 和 secret。它们必须属于签发 refresh token 的客户端；本项目不内置第三方客户端凭据，不轮流尝试其他客户端。自行注册的新客户端不保证拥有 Antigravity 权限。
2. 在供应商页创建「Antigravity OAuth（实验性）」供应商，然后导入 Antigravity-Manager 导出的账号 JSON。支持 `{"accounts":[{"email":"user@example.com","refresh_token":"请替换"}]}`、单个账号项及含 `token.refresh_token` 的完整账号文件；可附带 `client_id` 检查绑定。上传限 1 MiB，每个文件最多 100 个账号，批量导入分别返回成功项和失败原因。文件中的身份和项目不作为鉴权依据。
3. 导入会刷新令牌、验证 Google 身份，并通过 `loadCodeAssist` 查询项目。账号应先在 Antigravity 完成初始化；没有真实项目时明确失败。完整账号文件中的 `oauth_client_key` 是外部应用的本地标识，不能代替 OAuth client ID；缺少 `client_id` 时使用运行配置中的唯一客户端。
4. 创建虚拟模型候选，选择 Antigravity 上游入口，填写真实模型名及输出 Token 限制。可手动拉取模型目录；不会自动创建路由。客户端继续使用现有 Chat Completions、Responses 或 Messages 入口。

本版本支持文本、SSE、非流式聚合、function 工具定义及无签名的工具历史、base64/data URL 图片输入。远程图片 URL、显式推理配置、受保护思考历史、要求 `thoughtSignature` 的工具调用、Responses 的 custom/shell/apply_patch 工具、`strict:true` 和禁止并行工具的控制暂不支持；无法转换时明确报错，不静默删减。不能据此承诺 Codex、Claude Code 默认参数可直接使用，也不能承诺所有订阅模型均兼容。上游协议、参数校验及终止语义以[请求适配](../internal/protocol/antigravity_request.go)和[响应适配](../internal/protocol/antigravity_response.go)为准。

上游始终使用 SSE；非流式客户端收到聚合 JSON。401 在首内容前最多触发一次刷新重发；429 按账号与真实模型保存冷却，优先读取结构化 `RetryInfo` 或 `Retry-After`，无恢复信号时冷却一分钟。上游健康页显示模型冷却，可清除此账号的模型冷却。刷新不会开启手工停用的账号；明确的 `invalid_grant` 或刷新后持续 401 需要重新导入。删除账号清空当前凭据并归档，历史载荷仍按日志保留策略保存。

生成请求和 Authorization access token 遵循既有明文日志契约；OAuth 刷新正文、refresh token 和完整导入文件不进入模型载荷或运行日志。浏览器登录见上文；IDE 账号切换及签名历史存储不在本版本内。

### Codex OAuth 账号

1. 在外部 Codex CLI 登录 ChatGPT，并使用文件存储获得 `auth.json`；codex-deck 导出相同格式也可导入。CLI 文件存储配置见 [官方认证说明](https://learn.chatgpt.com/docs/auth)。建议为网关使用独立登录会话，避免 CLI、codex-deck 和网关争用 refresh token。
2. 在供应商页选择「Codex OAuth」，填写名称并保存。随后上传 auth.json，批量选择会逐个上传。上传请求限 1 MiB，格式、身份或重复账号错误会明确显示；导入成功表示格式可读，不代表模型权限已通过验证。
3. 创建虚拟模型候选，选择该供应商及 Responses 上游入口，手工填写真实模型名。Codex 不提供模型列表拉取和输出 Token 限额配置。使用虚拟模型连通性测试检查实际路由，客户端可继续使用现有三个入口。

网关将当前凭据保存在 SQLite，按需刷新，不读取或回写用户本地文件。也可按上文直接在后台发起浏览器登录。访问令牌到期不等于账号永久失效。鉴权失效时，删除旧账号后重新登录或导入；运行状态重置无法修复永久失效的刷新令牌。不提供原位替换令牌、凭据编辑、复制或导出。

账号支持启停与删除，可删除最后一个账号。同用户、工作区的未删除账号在实例内不能重复上传，也不能用上传覆盖令牌。删除清空当前凭据并保留历史引用，不撤销 OpenAI 会话。模型历史日志可能保存旧 access token，继续按载荷保留策略处理。

Codex 忽略长度、采样等明确列出的参数，集合与边界见 [供应商例外](requirements.md#63-严格转换与供应商例外)。详情展示本次忽略路径、实际上游 SSE 和最终客户端响应。非流式客户端收到聚合 JSON；服务端会话字段仍受无状态边界限制。

## 数据与远程访问

数据库位于数据目录下的 `model-confluence.db`，同目录可能出现 WAL 文件。请求正文压缩不等于加密；数据库、日志下载及截图都可能包含提示词、代码和完整凭据。限制数据目录的系统访问权限，不将这些内容提交到 Git。

管理会话使用 Secure、SameSite Strict Cookie 和 CSRF 防护。远程访问应使用 HTTPS 反向代理，并将管理后台与 `/api` 保持同源；普通远程 HTTP 地址无法正常维持登录态。本地开发使用 README 中的 localhost 地址。

客户端 IP 当前只从可信 TCP 对端提供的 `X-Forwarded-For` 解析；代理不在 `--trusted-proxies` 中时使用 TCP 对端地址。请让反向代理正确处理该头，不要依赖 `Forwarded`。实现见 [客户端 IP 解析](../internal/httpx/httpx.go)。

忘记管理员密码时，先停服，再对原数据目录执行重置；所有已有管理员会话随之撤销。Windows 二进制示例：

```powershell
$env:MODEL_CONFLUENCE_ADMIN_PASSWORD = "新的管理员密码"
./model-confluence.exe admin reset-password --data-dir ./data
```

macOS / Linux：

```bash
MODEL_CONFLUENCE_ADMIN_PASSWORD="新的管理员密码" ./model-confluence admin reset-password --data-dir ./data
```

## 开发与构建

开发启动步骤见 [README](../README.md#本地开发)。依赖版本以 [go.mod](../go.mod)、[package.json](../web/package.json) 和 pnpm 锁文件为准，构建使用的 Node.js / pnpm 版本见发布工作流。Vite 将 `/api` 和 `/healthz` 代理到后端，配置在 [vite.config.ts](../web/vite.config.ts)；模型 API 可直接调用后端地址。

构建含管理后台的 Windows 单文件版本：

```powershell
cd web
pnpm install --frozen-lockfile
pnpm build
cd ..
go build -tags embedded_ui -o model-confluence.exe ./cmd/model-confluence
```

前端构建将资源写入 `internal/webui/dist`，随后由 Go 嵌入。不要手工编辑该目录；普通 `go run` / `go build` / `go test` 不带 `embedded_ui` 时使用占位页，无需前端产物。运行正式版本不依赖 Node.js。

按变更影响选择验证：后端相关包使用 `go test ./internal/<package>`，前端使用 `pnpm --dir web build`；需要整体后端验证时运行 `go test ./...`。文档和注释变更核对事实、链接、示例及最终 diff，无需编译或重建资源。协议转换验证应覆盖受影响方向的请求、响应、流式和多轮历史，不能用单次连通性测试代替。

## 发布

发布由 `vX.Y.Z` Git 标签触发，标签应指向已推送到 `main` 的提交。工作流重新构建前端、运行 Go 测试、构建嵌入后台的二进制归档和多架构容器、生成校验文件并创建 GitHub Release；发布目标、工具版本和构建命令以 [发布工作流](../.github/workflows/release.yml) 与 Dockerfile 为准。

仅在明确执行发布任务时创建和推送版本标签。管理员密码和供应商凭据不进入构建或发布产物，部署时再注入。
