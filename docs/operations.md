# 运行与开发

本文档说明如何部署、配置、调用和开发模汇。项目介绍见 [README](../README.md)，产品契约和实现差距见 [需求与设计](requirements.md)。

## 二进制与首次启动

启动命令见 [README](../README.md#下载与运行)。下载适合当前系统和架构的归档，发布页面同时提供 `checksums.txt`。归档目标与命名见 [发布工作流](../.github/workflows/release.yml)。

应用在数据库尚未建立管理员时使用初始密码，随后保存密码哈希。已有实例需要更换密码时，请使用后台修改密码功能，或按 [密码重置步骤](#数据与远程访问)操作。

## Docker 与 Compose

镜像使用非 root 用户。以下示例将容器的 8080 端口发布到宿主机回环地址，使用命名卷挂载 `/data`。容器默认参数和发布架构见 [Dockerfile](../Dockerfile) 与 [发布工作流](../.github/workflows/release.yml)。

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

下表对应 [配置定义](../internal/config/config.go)。命令行参数覆盖对应的环境变量默认值。若设置了 `MODEL_CONFLUENCE_LOG_RETENTION_DAYS`，它必须能解析为整数，即使同时传入 `--log-retention-days`。时长参数使用 `10s`、`5m` 等形式。

| 参数 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `--listen` | `MODEL_CONFLUENCE_LISTEN` | `127.0.0.1:8080` | HTTP 监听地址；容器启动参数另行指定 |
| `--data-dir` | `MODEL_CONFLUENCE_DATA_DIR` | `data` | 数据目录；相对路径以启动工作目录为基准 |
| `--admin-password` | `MODEL_CONFLUENCE_ADMIN_PASSWORD` | 空 | 初始或重置后的管理员密码 |
| `--trusted-proxies` | `MODEL_CONFLUENCE_TRUSTED_PROXIES` | 空 | 逗号分隔的可信代理 CIDR |
| `--connect-timeout` | — | `10s` | 上游连接超时 |
| `--response-header-timeout` | — | `5m` | 等待上游响应头超时 |
| `--stream-idle-timeout` | — | `5m` | 读取成功状态的上游生成响应时允许的空闲时间，适用于流式和非流式 |
| `--stream-heartbeat-interval` | — | `0` | SSE 心跳间隔，`0` 关闭，不能为负数 |
| `--max-request-bytes` | — | `67108864` | 入站生成请求体上限（64 MiB），必须大于 0 |
| `--log-retention-days` | `MODEL_CONFLUENCE_LOG_RETENTION_DAYS` | `0` | 完整日志载荷保留天数；`0` 永久保留，不能为负数 |
| `--session-affinity` | — | `false` | 按客户端 `X-Session-ID` 在同一候选内优先复用上次成功的密钥 |
| — | `MODEL_CONFLUENCE_ANTIGRAVITY_OAUTH_CLIENT_ID` | 空 | 签发 Antigravity refresh token 的 OAuth 客户端 ID |
| — | `MODEL_CONFLUENCE_ANTIGRAVITY_OAUTH_CLIENT_SECRET` | 空 | 上述 OAuth 客户端的 secret，只在运行时注入 |

心跳不会重置上游读取空闲计时。普通供应商开启心跳后可能在模型输出前发送 HTTP `200`，客户端需通过 SSE 识别后续错误。提交和切换规则见 [流式提交与取消](requirements.md#8-流式提交与取消)。

设置保留天数后，服务在启动时及后台定期按请求创建时间清理过期载荷。需要按时间范围预览、清空载荷或永久删除记录时，使用「使用记录 → 存储管理」。保留范围与分批清理规则见 [载荷生命周期](requirements.md#115-载荷生命周期)。

开启 `--session-affinity` 后，在同一会话的请求中发送相同的 `X-Session-ID`，最多 512 字节。密钥选择规则见 [上游密钥](requirements.md#33-上游密钥)。

## 管理后台与客户端调用

登录管理后台后创建访问密钥。普通供应商填写完整协议端点和上游密钥，也可通过「应用模板」填入名称和端点。OAuth 供应商先点击「创建并添加账号」，再登录或导入凭据。随后创建虚拟模型，添加并排序候选。客户端的 `model` 使用虚拟模型名。

编辑候选时可手工填写真实模型名。普通 API Key 和 Antigravity 供应商也可点击「拉取模型」，再从输入框选择。目录按需查询，作用是辅助填入模型名。模型权限需要通过连通性或兼容性验证确认。

展开模型路由可按协议、流式、工具及并行工具条件预演，查看候选和密钥的跳过原因。实际请求的选择过程见使用记录中的路由快照。要查看当前进程的冷却与探测状态，请打开「上游健康」。

「实时路由」页可见时轮询刷新请求状态。开启「显示空闲节点」可同时查看启用的空闲配置。选择节点可筛选请求，选择请求 ID 可查看阶段、尝试与切换记录，并打开完整使用记录。「最近结束」最多显示当前进程在过去 5 分钟内结束的 10 条请求。

模型测试弹窗中的「兼容性验证」可指定候选、上下游协议和流式方式。工具场景连续进行两轮工具调用，再验证最终文本。验证会真实调用上游，最长两分钟，可从结果中的请求 ID 查看详情。普通连通性测试由当前路由选择上游，适合检查虚拟模型是否可调用。

在「使用记录」中通过「更多筛选」缩小范围，再从请求详情打开「转换差异」或「调试重放」。重放可编辑原协议正文，使用当前路由真实调用上游并生成关联记录，最长两分钟。筛选口径、重放可用条件及凭据处理见 [使用记录页](requirements.md#123-使用记录页)。

### 模型 API 示例

示例中将 `mc_your_access_key` 换为后台创建的访问密钥，将 `your-virtual-model` 换为配置好的虚拟模型。各协议入口与鉴权规则见 [对外 API](requirements.md#4-对外-api)。

macOS / Linux 可用 curl 查询模型列表和调用 Chat Completions。

```bash
curl http://127.0.0.1:8080/v1/models \
  -H "Authorization: Bearer mc_your_access_key"

curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer mc_your_access_key" \
  -H "Content-Type: application/json" \
  -d '{"model":"your-virtual-model","messages":[{"role":"user","content":"你好"}],"stream":false}'
```

Windows 可用 PowerShell 7.3 或更新版本运行以下命令。JSON 参数使用默认的原生命令参数传递方式。见 [PowerShell 参数解析](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_parsing#passing-arguments-that-contain-quote-characters)。

```powershell
curl.exe http://127.0.0.1:8080/v1/models `
  -H "Authorization: Bearer mc_your_access_key"

curl.exe http://127.0.0.1:8080/v1/chat/completions `
  -H "Authorization: Bearer mc_your_access_key" `
  -H "Content-Type: application/json" `
  -d '{"model":"your-virtual-model","messages":[{"role":"user","content":"你好"}],"stream":false}'
```


### 浏览器登录

在 OAuth 供应商的账号列表点击「登录添加账号」，从弹窗打开 OpenAI 或 Google 授权页。授权成功后账号会出现在列表中。

远程或 Docker 部署可使用回调 URL 回填。保持登录弹窗打开，在供应商页面授权。若浏览器提示无法连接 `127.0.0.1`，复制地址栏的完整 URL，粘贴到模汇的「完整回调 URL」，点击「提交回调」。回填与自动回调共用一次性校验，具体边界见 [上游密钥](requirements.md#33-上游密钥)。

| 供应商 | 临时回调地址 |
| --- | --- |
| Codex OAuth | `http://127.0.0.1:1455/auth/callback` |
| Antigravity OAuth | `http://127.0.0.1:51121/oauth-callback` |

每个管理员会话只能发起一个未完成的登录，5 分钟后过期。端口被占用时，先结束使用该回调端口的程序或登录流程。服务重启后需要重新发起登录。

本机浏览器与服务进程处于同一网络环境时，可直接自动回调。远程浏览器中的 `127.0.0.1` 指向用户电脑；若希望远程自动回调，也可先建立 SSH 隧道，并保持连接，再从远程后台发起登录：

```bash
ssh -N -L 1455:127.0.0.1:1455 -L 51121:127.0.0.1:51121 user@server
```

回调服务绑定进程所在网络命名空间的回环地址。Docker 桥接部署使用回调 URL 回填，因为普通端口映射无法到达容器回环。Linux 主机网络部署可按上例转发。模汇支持本机自动回调和手动回填。

Antigravity 登录仍需配置下文的 OAuth 客户端，并确认它允许表中的回调地址及所需 scope；授权请求包含 `openid`、`userinfo.email` 和 `cloud-platform`，要求离线访问。交换令牌后还会验证 Google 身份和查询项目；缺少 refresh token 或项目时明确失败。配置客户端不代表拥有订阅模型权限。

已有账号鉴权失效时，在账号展开区选择「重新登录」，使用原 Codex 用户与工作区或原 Antigravity 用户与项目授权，以保留配置和历史关联。`auth_invalid` 的 OAuth 账号需要新凭据。

在账号展开区可查看最近的额度与限流信号及其来源请求。

### Antigravity OAuth

此接入为实验性能力。仓库测试与历史线上验收见 [供应商验证边界](requirements.md#172-供应商验证边界)。

Google Antigravity 附加条款第 6 条将通过第三方软件、工具或服务访问列为违约，可能导致 Antigravity 或 Gemini CLI 账号暂停或终止。该页面也列出适用其他条款的企业接入情形。见 [Google Antigravity 附加条款](https://antigravity.google/terms)。

1. 在运行环境注入上述 OAuth client ID 和 secret。它们必须属于签发 refresh token 的客户端；本项目不内置第三方客户端凭据，不轮流尝试其他客户端。自行注册的新客户端不保证拥有 Antigravity 权限。
2. 在供应商页创建「Antigravity OAuth（实验性）」供应商。可使用浏览器登录，或导入 Antigravity-Manager 导出的账号 JSON。支持 `{"accounts":[{"email":"user@example.com","refresh_token":"请替换"}]}`、单个账号项及含 `token.refresh_token` 的完整账号文件。每次上传请求含一个文件，multipart 请求体上限为 1 MiB，每个文件最多 100 个账号。
3. 账号需先在 Antigravity 完成初始化。导入会向 Google 验证身份和查询项目，文件中的账号、项目及 `oauth_client_key` 无法替代这项校验。文件若提供 `client_id`，必须与运行配置一致。
4. 创建虚拟模型候选，选择 Antigravity 上游入口，填写真实模型名和输出 Token 限制。客户端沿用 [现有模型 API](requirements.md#4-对外-api)。

支持文本、SSE、非流式聚合、function 工具和 base64/data URL 图片。多轮工具调用时，客户端按原协议回传响应文本、完整 assistant 工具调用和工具结果。并行调用须保留在同一条 assistant 消息中。签名历史及路由绑定规则见 [供应商例外](requirements.md#63-严格转换与供应商例外)。

远程图片 URL、显式推理配置、客户端直接提交的受保护思考历史、Responses 的 custom/shell/apply_patch 工具、`strict:true` 和禁止并行工具的控制暂不支持；无法转换时明确报错，不静默删减。不能据此承诺 Codex、Claude Code 默认参数可直接使用，也不能承诺所有订阅模型均兼容。上游协议、参数校验及终止语义以[请求适配](../internal/protocol/antigravity_request.go)和[响应适配](../internal/protocol/antigravity_response.go)为准。

上游健康页显示账号的模型冷却，可按账号清除。401、429 和刷新失败的处理见 [上游失败与故障切换](requirements.md#9-上游失败密钥池与故障切换)。

### Codex OAuth 账号

1. 在外部 Codex CLI 登录 ChatGPT，并使用文件存储获得 `auth.json`；codex-deck 导出相同格式也可导入。CLI 文件存储配置见 [官方认证说明](https://learn.chatgpt.com/docs/auth)。建议为网关使用独立登录会话，避免 CLI、codex-deck 和网关争用 refresh token。
2. 在供应商页选择「Codex OAuth」，填写名称并保存。可使用浏览器登录，或上传 `auth.json`。批量选择时逐个上传，每次 multipart 请求体上限为 1 MiB。
3. 创建虚拟模型候选，选择该供应商及 Responses 上游入口，手工填写真实模型名。Codex 不提供模型列表拉取和输出 Token 限额配置。使用虚拟模型连通性测试检查实际路由，客户端可继续使用现有三个入口。

在请求详情可查看 Codex 忽略的参数路径、上游 SSE 和最终客户端响应。忽略条件与字段来源见 [供应商例外](requirements.md#63-严格转换与供应商例外)。

## 数据与远程访问

数据库位于数据目录下的 `model-confluence.db`，同目录可能出现 WAL 文件。限制数据目录与载荷导出的访问权限，提交截图时使用演示数据。明文凭据与载荷的存储约定见 [记录模型](requirements.md#111-记录模型)。

远程访问时，在 HTTPS 反向代理上为管理后台和 `/api` 使用同一域名。管理会话使用 Secure Cookie，本机可访问 `localhost`。会话与 CSRF 规则见 [登录](requirements.md#121-登录)。

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

Go 与前端依赖分别见 [go.mod](../go.mod)、[package.json](../web/package.json) 和 [pnpm 锁文件](../web/pnpm-lock.yaml)。Node.js 与 pnpm 版本见 [发布工作流](../.github/workflows/release.yml)。

在项目根目录启动开发后端。macOS / Linux 使用以下命令。

```bash
MODEL_CONFLUENCE_ADMIN_PASSWORD="请替换为管理员密码" go run ./cmd/model-confluence --listen 127.0.0.1:8080
```

Windows PowerShell 使用以下命令。

```powershell
$env:MODEL_CONFLUENCE_ADMIN_PASSWORD = "请替换为管理员密码"
go run ./cmd/model-confluence --listen 127.0.0.1:8080
```

另开一个终端，在项目根目录运行以下命令。

```text
pnpm --dir web install --frozen-lockfile
pnpm --dir web dev
```

用浏览器打开 Vite 输出的本机地址。Vite 将管理 API 和健康检查代理到后端，后端地址见 [Vite 配置](../web/vite.config.ts)。模型 API 直接访问后端。

构建含管理后台的 Windows 单文件版本：

```powershell
cd web
pnpm install --frozen-lockfile
pnpm build
cd ..
go build -tags embedded_ui -o model-confluence.exe ./cmd/model-confluence
```

前端产物由 Vite 生成，再通过 `embedded_ui` 标签嵌入 Go 程序。普通 Go 开发与测试使用占位页。构建路径见 [Vite 配置](../web/vite.config.ts) 和 [资源嵌入](../internal/webui/embed.go)。

后端可用 `go test ./internal/<package>` 或 `go test ./...` 验证，前端使用 `pnpm --dir web build`。协议转换的验收场景见 [核心验收目标](requirements.md#16-核心验收目标)。

## 发布

程序版本以 [web/package.json](../web/package.json) 为源，使用 [版本工具](../scripts/version.mjs) 更新与检查。每次发布前，按上个正式标签至候选提交的代码变化编写 `docs/releases/<version>.md`，使用中文描述用户可见效果，按内容选择新增、改进、修复、升级说明或已知限制等分组，删除空分组。涉及协议转换时说明协议方向和适用场景；涉及配置、持久化或兼容变化时说明必要操作与限制。

在仓库根目录执行以下命令，变量替换为拟发布的新版本号。示例使用 sh 兼容 Shell：

```sh
next_version='<新版本号>'
pnpm --dir web bump "$next_version"
pnpm --dir web check:version "v$next_version"
pnpm --dir web test:version
```

`bump` 先检查正式三段数字版本及对应非空日志，再更新版本字段；`check:version` 校验标签、版本文件与日志。工具不会自动提交或创建标签。日志与版本文件应放在同一个发布准备提交中，按改动完成验证后推送至 `main`。

获得发布授权后，在已检查且已推送至 `main` 的提交创建并推送标签：

```sh
git tag -a "v$next_version" -m "Release v$next_version"
git push origin "v$next_version"
```

发布由 `vX.Y.Z` Git 标签触发。工作流从源码构建、测试并打包，使用仓库版本日志作为 Release 正文。草稿机制允许重跑时补齐未公开版本的附件，公开后需发布新版本。容器镜像先于 GitHub Release 发布。执行顺序见 [发布工作流](../.github/workflows/release.yml)。

仓库日志从 1.9.4 开始维护，较早版本继续查看 [GitHub Releases](https://github.com/Sanjeever/model-confluence/releases)。已公开日志保留当时语义，后续修复通过新版本交付。

管理员密码和供应商凭据在部署时注入。
