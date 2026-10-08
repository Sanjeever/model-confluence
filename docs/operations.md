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

上游生成请求不设置总时长上限。心跳只能维持客户端连接，不会重置上游读取空闲超时；开启心跳后，HTTP 响应可能在收到模型内容前已发送 `200`，后续错误只能以 SSE 表达。

设置保留天数后，服务启动时及后台定期清理已结束请求的过期正文、头部和错误详情，保留请求与 attempt 元数据及 usage，并标记载荷已清理；过期精简安全事件会删除。该选项不会删除配置中的访问密钥或供应商密钥，也不提供按时间范围手工删除使用记录。

`GET /healthz` 无需鉴权，仅检查 SQLite 可访问，不探测上游连通性。定义见 [HTTP 装配](../internal/app/app.go)。

## 管理后台与客户端调用

登录管理后台后，创建访问密钥，配置供应商完整协议端点和上游密钥，再创建虚拟模型及有序候选。客户端的 `model` 使用虚拟模型名。

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

配置候选时可以手动拉取供应商模型名；该功能从已配置的生成端点推导模型列表地址，供应商需提供兼容的列表 API，不是自动同步。管理后台的连通性测试针对虚拟模型，经过实际路由选择、转换、密钥池及日志流程；一次成功只说明本次选中的路由可用，不验证所有候选、协议或流式能力。

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
