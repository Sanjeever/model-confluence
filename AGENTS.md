# AGENTS.md

本文档约束在 model-confluence 仓库中工作的自动化编码代理。开始修改前先阅读 `README.md`；涉及产品边界、协议语义或安全取舍时，再阅读 `docs/requirements.md`。README 描述当前实现，需求书包含目标设计，两者不一致时不要擅自补齐未被用户要求的功能。

## 沟通与范围

- 使用中文沟通，代码、协议字段、命令和提交信息除外。
- 按用户要求做最小闭环修改，不顺手重构、补文档、升级依赖或扩展功能。
- 先定位真实失败点，再修改最小范围；不要添加静默降级、假数据或吞错重试。
- 保留用户工作区中的现有改动，不执行破坏性 Git 或文件操作。

## 项目结构

- `cmd/model-confluence`：程序入口和优雅退出。
- `internal/admin`：管理 API、管理员会话、CSRF 和登录限速。
- `internal/gateway`：入站鉴权、路由、上游请求、流式代理和日志收尾。
- `internal/protocol`：Chat Completions、Responses、Messages 的规范模型和双向转换。
- `internal/store`：SQLite schema、兼容迁移、配置、路由解析和请求日志。
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

## 协议与路由约束

- 协议常量统一使用 `chat_completions`、`responses`、`messages`。
- 同协议走治理式透传，只改鉴权、模型名和必要头部；不要无故重编码未知字段。
- 跨协议先解码到 `internal/protocol` 的受限规范模型，再编码到目标协议。
- 请求和响应转换必须对称；新增响应块时，要检查它是否会被客户端放回下一轮请求。
- 流式转换通过规范事件完成。首个有效内容写出后不能切换到另一个上游响应。
- Chat 的 `reasoning_content`、Responses reasoning 和 Messages thinking 的映射要同时考虑流式与非流式、多轮历史和工具调用。
- Anthropic `signature`、`redacted_thinking` 等目标协议无法表达的字段不能伪造成 Chat 或 Responses 字段。
- 路由由虚拟模型候选顺序决定；候选内部优先同协议，否则按协议入口顺序选择。
- 模型候选只能引用供应商已配置的协议端点；删除端点或供应商前必须检查模型路由引用。

## 日志与敏感数据

- 一个 `requests` 记录对应一个入站请求，一个 `attempts` 记录对应一次上游尝试。
- 新日志字段要同时更新写入、列表扫描、详情扫描、JSON 类型和前端类型。
- 流式与非流式 usage 都从上游原始响应解析；上游未返回的 Token 指标保持 `null`，不能估算或伪造为零。
- 项目按明确需求保存并向管理员展示完整访问密钥和供应商密钥。不要擅自改回脱敏 API，也不要在普通运行日志或提交信息中输出真实密钥。
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
