# AGENTS.md

本文档约束在 model-confluence 仓库中工作的自动化编码代理。开始修改前先阅读 `README.md`；涉及产品边界、协议语义或安全取舍时，再阅读 `docs/requirements.md`。README 描述当前实现，需求书包含目标设计，两者不一致时不要擅自补齐未被用户要求的功能。

## 沟通与范围

- 使用中文沟通，代码、协议字段、命令和提交信息除外。
- 按用户要求做最小闭环修改，不顺手重构、补文档、升级依赖或扩展功能。
- 先定位真实失败点，再修改最小范围；不要添加静默降级、假数据或吞错重试。
- 保留用户工作区中的现有改动，不执行破坏性 Git 或文件操作。
- 需求书明确冻结的边界不要擅自补齐：不实现 JSONL 导出、按时间范围删除记录、精简安全事件独立视图、使用记录多维结构化筛选、CORS、多用户/RBAC/计费、外部数据库/横向扩容、供应商模型自动同步等；涉及这些先向用户确认。

## 项目结构

- `cmd/model-confluence`：程序入口和优雅退出。
- `internal/admin`：管理 API、管理员会话、CSRF 和登录限速。
- `internal/app`：HTTP 装配，注册 healthz、admin、gateway、webui 路由并套安全头和请求日志中间件。
- `internal/config`：CLI 参数与环境变量解析、子命令识别和校验。
- `internal/gateway`：入站鉴权、路由、上游请求、重试冷却、流式代理和日志收尾。
- `internal/httpx`：JSON 读写工具与基于可信代理的客户端 IP 解析。
- `internal/protocol`：Chat Completions、Responses、Messages 的规范模型和双向转换。
- `internal/store`：SQLite schema、兼容迁移、配置、路由解析、请求日志、用量统计和上游健康查询。
- `internal/webui`：通过 `embed` 托管 `dist`。
- `web/src`：React 管理后台源码。
- `.github/workflows/release.yml`：版本标签触发的二进制与容器发布流程。
- `Dockerfile`：Linux `amd64`、`arm64` 多架构容器构建。
- `docs/requirements.md`：首版产品需求与长期设计边界。

## 开发命令

后端开发：

```powershell
$env:MODEL_CONFLUENCE_ADMIN_PASSWORD = "本地管理员密码"
go run ./cmd/model-confluence --listen 127.0.0.1:8080 --data-dir ./data
```

前端开发：

```powershell
cd web
pnpm install --frozen-lockfile
pnpm dev
```

前端构建产物写入 `internal/webui/dist`：

```powershell
cd web
pnpm build
```

只有用户要求验证时才运行相应检查：

```powershell
go test ./...
cd web
pnpm build
```

## 构建与发布约束

- 前端资源通过 `//go:build embedded_ui` 标签嵌入；不带该标签的 `go run`/`go build`/`go test` 编译出占位页（无管理后台，不含前端）。本地只跑后端不依赖它，正式构建和发布必须带 `-tags embedded_ui`。
- 正式发布以 `vX.Y.Z` Git 标签为唯一触发入口，由 `.github/workflows/release.yml` 创建 GitHub Release 和 GHCR 镜像。
- 发布工作流必须先重新构建 `internal/webui/dist`，再执行 Go 测试和交叉编译；不能直接信任仓库中的旧前端产物。
- 二进制发布目标为 macOS `arm64`、Windows `amd64` 和 Linux `amd64`；Docker 镜像目标为 Linux `amd64`、`arm64`。
- Docker 容器内监听 `0.0.0.0:8080`，SQLite 数据固定保存在 `/data`，最终镜像使用非 root 用户运行。
- 管理员密码和其他凭据只能在运行容器时注入，禁止写入 Dockerfile、镜像层、GitHub Actions 或发布产物。
- 未经用户明确要求，不创建、移动、覆盖或推送版本标签，不修改已经发布的 Release。

## 后端约束

- Go 代码使用 `gofmt`，错误应带明确上下文并尽早返回。
- HTTP 路由使用 Go `http.ServeMux` 的方法路径模式，不额外引入 Web 框架。
- SQLite 字段名必须使用全小写 `snake_case`。
- schema 位于 `internal/store/migrations.go`。新增字段时同时更新新建表定义，并在 `store.migrate` 中补充已有数据库的事务迁移。
- SQLite 使用 WAL，但不支持多个实例共享数据库；不要在测试或脚本中操作用户的 `data` 目录。
- 配置保存和使用记录写入失败时应明确失败，不能产生已知的无日志上游调用。
- 日志只使用标准库 `log/slog`；网关与上游调用不打 stdout，业务日志写入 SQLite（`requests`/`attempts`/`security_events`）。
- 哨兵错误就地 `var ... = errors.New(...)` 声明，用 `fmt.Errorf("...: %w", err)` 包装上下文；`store` 层用 `sql.ErrNoRows` 表示不存在，调用方 `errors.Is` 判断。
- 测试用标准库 `testing`，表驱动 + `t.Run`；上游用 `httptest.NewServer` 模拟，SQLite 用 `t.TempDir()` 临时文件（不触碰用户 `data` 目录）。
- `/healthz` 只代表进程存活 + SQLite 可 Ping，不探测上游、不返回供应商配置。
- 管理会话 Cookie 为 `mc_session`(HttpOnly) 与 `mc_csrf`（非 HttpOnly），均 `Secure: true + SameSite: Strict`；非 localhost 的 HTTP 直连会因 Secure 拿不到登录态。

## 协议与路由约束

- 协议常量值统一为 `chat_completions`、`responses`、`messages`，以 `internal/protocol` 的 `Chat`/`Responses`/`Messages` 为唯一来源；新增引用协议名的代码用包常量，不要重新定义字面量。
- 同协议走治理式透传，只改鉴权、模型名和必要头部；不要无故重编码未知字段。
- 跨协议先解码到 `internal/protocol` 的受限规范模型，再编码到目标协议。
- 跨协议只接受明确支持的字段，未知非空字段在上游调用前拒绝；入站 JSON 用 `DisallowUnknownFields` 严格解码，多传字段直接 `400`。
- 两类非语义信息允许跨协议丢弃并记日志：源协议 Beta/版本头（同协议透传、跨协议不转发且不因存在而拒）、缓存提示（`cache_control`/`prompt_cache_key` 跨协议记录不映射）。不要把这些静默丢弃当 bug 修。
- 请求和响应转换必须对称；新增响应块时，要检查它是否会被客户端放回下一轮请求。
- 跨协议工具调用 ID 由网关生成安全 ID；同协议保留上游对象 ID，跨协议生成目标形状对象 ID 并保存源/目标 ID。
- 图片跨协议支持 URL、data URL、base64，不支持供应商私有 `file_id`；转 Messages 时 `detail` 未设置或 `auto` 可省略，`low`/`high` 须在上游调用前拒绝。
- 流式转换通过规范事件完成。首个有效内容写出后不能切换到另一个上游响应。
- Chat 的 `reasoning_content`、Responses reasoning 和 Messages thinking 的映射要同时考虑流式与非流式、多轮历史和工具调用。
- 已知边界：response 侧 reasoning 完整映射，request 侧仅 Messages→Chat 的 thinking 会保留；Chat 自身多轮历史中的 `reasoning_content` 与跨协议编码到 Responses/Messages 的 reasoning 块会被丢弃（与需求书「thinking 跨协议往返不支持」一致，但同协议历史也会丢）。改这段前先确认是否属预期。
- Anthropic `signature`、`redacted_thinking` 等目标协议无法表达的字段不能伪造成 Chat 或 Responses 字段。
- 已知边界：候选的 `max_output_tokens` 上限目前只在管理端校验，网关请求侧未对超限值拒绝或钳制；客户端未给值时仅 Messages 入口按候选默认值填充。
- 路由由虚拟模型候选顺序决定；候选内部优先同协议，否则按候选配置的协议入口 `position` 顺序取第一个合格项，不是固定三协议优先级。
- 候选内部的上游密钥池按 `last_used_at` 做 least-recently-used 轮询（最久未用排前，时间相同保持池内顺序）；成功命中后更新 `last_used_at` 并重排。
- 候选连续 3 次连接失败/超时/`5xx` 进入 30 秒内存冷却，冷却结束允许一次探测；自动重试指数退避、单次上限 2 秒。候选冷却不持久化，密钥鉴权失效/额度耗尽与候选 `config_error` 才持久化。
- 模型候选只能引用供应商已配置的协议端点；删除端点或供应商前必须检查模型路由引用。
- 火山方舟（2026-09 升级后）的 `cache_creation_input_tokens` 只存在于其 Messages 协议 usage 顶层（Anthropic 风格），Chat/Responses 不返回该字段。网关对 Messages 已映射到 `cache_write_tokens`；Chat/Responses 不采集属正确行为，勿再追加解析。
- 火山方舟 Responses 当前默认返回明文 `reasoning`/`summary`，网关跨协议转换正常；网关不识别 `reasoning.encrypted_content`，若方舟未来某场景只返回加密、不再返回明文，跨协议思考内容会丢失——属已知潜在边界，不作为 bug 处理。
- 火山方舟升级后，Messages 协议解码路径对 thinking/tool_use/tool_result 的字段解析由「报错」改为「静默兼容」，`redacted_thinking` 与流式 `signature_delta` 直接丢弃；Chat/Responses 的非法 content 与非支持模态仍在上游调用前报错。网关依赖上游报错暴露问题并触发故障切换的链路在 Messages 侧这几类上变弱。

## 日志与敏感数据

- 一个 `requests` 记录对应一个入站请求，一个 `attempts` 记录对应一次上游尝试。
- 新日志字段要同时更新写入、列表扫描、详情扫描、JSON 类型和前端类型。
- 流式与非流式 usage 都从上游原始响应解析；日志层缺失的 Token 指标保持 `null` 不估算、不补零，`total_tokens` 缺失时可由真实回包分量求和，客户端响应协议转换按规范补 0。禁止本地 tokenizer 估算。
- 项目按明确需求保存并向管理员展示完整访问密钥和供应商密钥。不要擅自改回脱敏 API，也不要在普通运行日志或提交信息中输出真实密钥。
- `requests`/`attempts` 的 `request_headers` 按原文保存，会内嵌访问密钥与供应商密钥明文；这是需求书已接受的风险，不要对这些头加脱敏。
- 未授权请求只记录精简安全事件，不保存其完整正文。

## 前端约束

- 使用 React、TypeScript、Ant Design、TanStack Query 和 Tailwind CSS。
- 界面以中文为主，协议名、JSON 字段、错误码等技术名称保留英文。
- 视觉保持克制的工业控制台风格，兼容浅色和深色主题，不增加装饰性英文标签。
- Ant Design 负责表单、表格、弹窗、分页和主题；Tailwind 主要负责布局、间距和少量展示。
- 查询条件必须进入 TanStack Query 的 `queryKey`；服务端分页、搜索或筛选不能只处理当前页数据。
- `web/src` 是前端源码。禁止手工编辑 `internal/webui/dist`，需要更新嵌入资源时运行 `pnpm build`。

## Git

- 默认分支为 `main`。
- 提交信息遵循 Conventional Commits，默认使用英文，例如：`feat: add protocol-aware AI gateway`。
- 发布标签使用 `vX.Y.Z` 格式，并且必须指向已经推送到 `main` 的提交。
- 提交前查看 `git status` 和 staged diff，确保不包含 `data`、数据库、密钥、临时文件、Node 依赖或本地可执行文件。
- 未经用户明确要求，不 amend、rebase、force push 或修改已有提交历史。
