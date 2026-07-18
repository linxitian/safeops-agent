# SafeOps Agent

[![CI](https://github.com/linxitian/safeops-agent/actions/workflows/ci.yml/badge.svg)](https://github.com/linxitian/safeops-agent/actions/workflows/ci.yml)

面向银河麒麟操作系统的安全自治智能运维智能体，中国软件杯 A2 赛题项目。

SafeOps 是自然语言与 Linux 运维能力之间的安全桥梁：它通过真实系统证据和 MCP 工具完成调查，并把受控写操作约束在“意图校验、风险评估、审批、最小权限执行、验证与回滚”的边界内。它不是普通聊天机器人，也不会把模型生成的 Shell 命令直接交给 Linux。

> 当前状态：七类统一 Collector、八视图中文 Web、六套 Benchmark、8 域 39 个 MCP Tool、双 Guard/Risk、完整 Trace、通用 Agent Runtime、证据图/BM25/RCA、审批/最小权限执行，以及端口、CPU、磁盘/日志、多轮文件和发布部署流程已通过官方银河麒麟 V11/LoongArch64 目标验证。运行时 `1a10880` 完成真实 Provider 与多轮写流程，后续版本完成默认数据保留卸载/配置连续性重装；候选运行时 `b5383e9`（PR #21 最终压缩合并为 `816f8cf`）通过已安装非 root Registry 对全部 39 个 Tool 的逐一结构化调用，`2b26de4`（PR #23 最终压缩合并为 `7479752`）完成真实 Chrome 八视图审计，合并版 `7479752` 又完成原生 39/39 Tool 复测与六套 Benchmark，候选运行时 `053fc2c`（PR #27 最终压缩合并为 `4861dcf`）完成 7/7 Collector 与两个传输适配模型的原生计数审计。候选提交、PR head 和 `main` 提交的完整谱系见[目标证据索引](docs/evidence/README.md)。

## 五大核心支柱

1. **OS 环境深度感知**：统一 Platform/Collector/Observation，采集真实 Linux 状态。
2. **MCP 运维插件化**：官方 MCP Go SDK、Manifest Registry、协议发现、真实版本识别与周期健康检查。
3. **安全意图校验**：版本化 Static Guard、Intent Guard 和上下文风险引擎，未知写能力 fail closed。
4. **最小权限代理执行**：Unix Socket 独立执行边界、签名 Envelope、重放/目标重验证，以及显式 Lab 模式中的固定白名单 Handler。
5. **推理链路溯源**：结构化决策摘要和 SHA-256 哈希链，不保存隐藏思维过程。

## 当前总体架构

```text
中文 React 控制台
  -> HTTP + SSE
  -> 持久化 Session / Task
  -> Agent Orchestrator
  -> MCP Plugin Registry
  -> 8 个独立 stdio MCP Server / 39 个 typed Tools
  -> LinuxPlatform / SafeFS / 固定参数 systemctl 与 journalctl
  -> /proc、statfs、网络、服务、日志、allowlist 文件与配置
  -> Evidence Graph + BM25 + D1-D3 RCA
  -> 结构化 Tool Result -> Agent Observe / Replan / 完成条件
  -> Action Proposal -> Guard/Risk -> Approval
  -> Unix Socket Privileged Executor -> Verification / Rollback
  -> JSON Session/Task + Hash-Chained JSONL Trace + Runtime JSONL Log
```

Static/Intent Guard 与 Risk 已接入只读和写动作准备链。Approval、ActionEnvelope、`safeops-privexec`、Verification、审批自动恢复和原子文件回滚已有集成测试；真实写仅存在于显式 `lab` 模式的固定 Handler，包含受控文件新建、可恢复删除、隔离/恢复、服务重启和进程 SIGTERM，且未作为 MCP Tool 暴露。主端口冲突以及 CPU、磁盘/日志和多轮文件受控处置计划已在官方目标机完成持久化跨审批闭环。详见 [SPEC.md](SPEC.md)、[PLAN.md](PLAN.md) 和 [目标验证审计](docs/target-verification-2026-07-18.md)。

## 技术栈

- Go 1.25+，默认 `CGO_ENABLED=0`
- 官方 `modelcontextprotocol/go-sdk` v1.6.1
- React + TypeScript + Vite
- SSE
- YAML 配置；JSON/JSONL 文件持久化
- OpenAI Compatible Provider（显式配置；Structured JSON 决策）

## 目录

```text
cmd/                 safeops-server、safeops-privexec 与 8 个只读 MCP Server
internal/platform/   集中的结构化 Linux 访问
internal/perception/ 七类 Collector、Observation 批次与 Prometheus/OTel 适配模型
internal/mcpservers/ MCP Server 实现
internal/registry/   MCP Manifest、发现、健康与调用
internal/agent/      纵切片、三个固定恢复状态机、通用有界 Runtime、动作准备与审批恢复
internal/context/    Selected Resources 的序号和指代解析
internal/storage/    Session/Task 原子 JSON Store
internal/trace/      Hash-Chained JSONL
internal/guard/      Static/Intent Guard 与上下文风险
internal/executor/   Envelope 再校验、nonce、Unix 客户端与固定 Handler
internal/rollback/   文件隔离清单、恢复和崩溃恢复
internal/benchmark/  六套件评测与报告生成
internal/evidence/   确定性证据图
internal/retrieval/  纯 Go BM25 与来源记录
internal/rca/        D1-D3 RCA 与置信度分量
internal/api/        HTTP/SSE API
config/              MCP Server Manifest
web/                 中文 React 控制台
scripts/             双架构构建、目标测试与发布入口
deploy/              目标配置、hardened systemd units、安装与保留数据的卸载入口
lab/                 受控端口、CPU、日志异常复现单元
docs/                架构差距、研究、覆盖矩阵
```

完整目标目录随 Milestone 逐步创建；缺失目录表示功能尚未实现，不是隐藏功能。

## 开发环境

- Linux；真实 `/proc` 测试仅在 Linux 执行
- Go 1.25 或更高
- Node.js 与 npm；当前验证版本为 Node.js 24，支持范围为 `^20.19.0 || ^22.12.0 || >=24.0.0`
- 推荐 `make`；若缺失，可直接运行 Makefile 中的底层命令
- `systemctl`、`journalctl`、`ss`、`ip` 用于固定参数感知与目标探测

当前开发机最初缺少 Go；本轮从 Go 官方下载并校验 SHA-256 后在用户目录安装了 Go 1.26.4。此安装不属于项目发布包。

## 快速开始

```bash
npm --prefix web ci
make test
make build-native
./bin/safeops-server \
  -listen 127.0.0.1:8080 \
  -data ./data \
  -mcp-config ./config/mcp_servers.yaml \
  -max-concurrent-tasks 8 \
  -max-sessions 1000 \
  -max-tasks 10000
```

API 未提供远程身份认证，因此进程只接受 loopback 监听地址；远程操作使用受控 SSH 转发或经过认证的本机反向代理。并发运行/审批恢复、持久 Session 和持久 Task 分别受以上硬上限约束：并发饱和返回 `429 Too Many Requests`，保留容量耗尽返回 `507 Insufficient Storage`，两者都不会修改持久状态。调整保留上限前应先规划审计数据的导出与清理流程。

另一个终端启动开发前端：

```bash
npm --prefix web run dev
```

打开 `http://127.0.0.1:5173`，输入：

```text
查看 CPU 和内存。
```

未配置 LLM 时，Agent 提供确定性的 CPU/内存只读纵切片。配置 Provider 后，通用 Runtime 只能选择 Registry 实际发现的 L0 Tool，并在本地校验 JSON Schema；每个 Tool Result 都重新进入 Runtime。Provider 已成功返回但决策 JSON 不符合契约时会携带有界错误摘要纠正一次，第二次仍无效则失败，网络与 HTTP 错误不会由该机制重试。模型只能在用户明确要求对应动作、相关 MCP 成功证据包含精确结构化目标身份后，申请 `service.restart` 或 `process.terminate` 两种固定受管动作；本地策略、目标快照和人工审批仍独立生效。写动作准备还需要显式提供执行器配置和 0600 HMAC secret，默认关闭。

## 配置与环境变量

MCP 注册配置为 `config/mcp_servers.yaml`。当前启用 system、process、network、journal、service、diagnostic、file、config 八个内建 Server。file/config 只能访问 Manifest 参数中的绝对 allowlist 根；配置工具不返回正文。

路径管控把目录元数据浏览根与文件写动作根分开：默认只读浏览覆盖 `/`，文件感知 MCP 仍只读 `/var/log` 与 `/var/lib/safeops/lab`；默认可写管理范围同时保留 `/home` 与既有 `/var/lib/safeops/lab`，避免破坏受控 Demo。资源管理器和新建目录使用目录句柄约束路径，符号链接不能逃逸配置根；新建目录由非 root server 完成，因此仍受目标目录 Unix 权限限制。

OpenAI Compatible Provider 使用：

```text
SAFEOPS_LLM_BASE_URL
SAFEOPS_LLM_API_KEY
SAFEOPS_LLM_MODEL
```

三个变量必须同时设置；代码不选择默认模型。未设置时不会创建 Provider，也不影响 CPU/内存纵切片。启用审批绑定的写动作准备还需启动参数：

```text
-executor-config ./config/executor.yaml
-executor-secret /path/to/0600-secret
-executor-socket /run/safeops/privexec.sock
```

Secret 至少 32 字节，不能允许 group/other 读取。`safeops-privexec` 默认 `dry-run`；只有显式 `-mode lab` 才注册受限 Lab Handler。

## 常用命令

```bash
make test                 # 前端组件/无障碍测试 + Go 测试
make lint                 # 前端类型检查 + go vet
make build-native         # 本机全部 Go command
make build-linux-amd64    # linux/amd64 静态构建
make build-loong64        # linux/loong64 静态交叉构建
make build                # amd64 + 前端生产构建
make target-test          # 本机或目标机只读 probe/test 报告
make benchmark            # 六套件 Benchmark 与固定报告
make release              # 全门禁、双架构构建、LoongArch64 发布包与 SHA256
```

`make target-test`、`make benchmark` 和 `make release` 已接入真实实现。Release 会重新执行 Go test/vet、前端组件/无障碍测试、lint/build 和 amd64/loong64 全 command 构建，再生成包内 `VERSION`、逐文件 `SHA256SUMS`、固定 tar.gz 与外层 SHA256。本开发主机没有安装 `make`，本轮使用 Makefile 中完全相同的底层命令验证。

## 当前 MCP Server 与 Tool

当前共 **8 个真实 MCP Server、39 个真实 MCP Tools**，均为 L0 只读，并已通过官方 SDK 协议测试：

- `system.get_overview`
- `system.get_cpu_metrics`
- `system.get_memory_metrics`
- `system.get_disk_usage`
- `system.get_load_average`
- `system.get_kernel_info`
- `system.get_mounts`
- `system.get_uptime`
- `process.list_top`、`process.search`、`process.get_details`
- `process.get_resource_usage`、`process.find_by_port`
- `network.list_listeners`、`network.list_connections`、`network.check_port`
- `network.get_interfaces`、`network.get_interface_stats`
- `journal.get_recent`、`journal.query_unit`、`journal.search_errors`、`journal.get_priority_events`
- `service.get_status`、`service.list_failed`、`service.get_dependencies`、`service.get_restart_count`
- `diagnostic.port_conflict`、`diagnostic.high_cpu`、`diagnostic.disk_pressure`、`diagnostic.build_snapshot`
- `file.list_roots`、`file.stat`、`file.list_directory`、`file.sha256`、`file.find_large`
- `config.list_roots`、`config.get_metadata`、`config.snapshot`、`config.diff_snapshot`

输入/输出由 typed `mcp.AddTool` 生成并校验 Schema。Registry 通过 stdio 完成 initialize、分页 tools discovery、schema/tool-set fingerprint、ping、enable/disable、rediscover 和 tools/call；它使用 initialize 响应中的真实实现名、版本与协议版本，不再把 Manifest 版本当成运行时版本。单一非重叠循环默认每 30 秒检查启用的 Server，每个 Server 最多 3 秒；绝对依赖只读取文件元数据，裸命令名只做可执行文件查找，绝不执行依赖字符串。API 与工具中心显示依赖状态以及最近 32 条健康/发现记录，失败详情有长度限制并脱敏，Tool 集变化会保留前后指纹。官方 Kylin 目标报告还以 `safeops` 非 root 身份对 39/39 Tool 逐一完成有界结构化调用，报告不持久化成功负载或配置正文。详见 [MCP 能力矩阵](docs/mcp-capability-matrix.md)。

## Demo 状态

### 当前可演示：CPU/内存真实感知

1. 在中文 Web 创建持久化会话。
2. 输入“查看 CPU 和内存”。
3. Agent 建立两步计划。
4. Registry 通过真实 MCP 调用 `mcp-system`。
5. LinuxPlatform 读取 `/proc/stat` 和 `/proc/meminfo`。
6. SSE 展示理解、采集、证据和完成阶段。
7. 刷新或重启服务后，会话、消息、Task 和 Trace 仍存在。

2026-07-16 的最新实际验证产生 22 个哈希链事件且完整性为 `VALID`。采样数值随主机实时状态变化。

### 已完成目标验证的受控 Demo

- 端口状态机在目标机完成固定 10 步计划：五个只读 Tool、D1/RAG、精确进程 L2 审批、独立服务重启 L1 审批，以及服务、端口、loopback HTTP 三重验证；87 事件 Trace 为 `VALID`。
- CPU 状态机在目标机完成 7/7，持久化基线与精确进程身份，审批后重新采样 CPU 并验证进程消失。磁盘/日志状态机完成 8/8，使用新文件快照和独立隔离审批，并明确隔离不等于释放同文件系统物理空间。
- 最终合并版在同一 Session 中完成大文件发现、有界上下文追问、第三项隔离和同记录恢复。追问只 `stat` 三个已选资源；四段 Trace 均为 `VALID`，恢复后的 2/3/4 MiB 文件身份与大小一致。

八视图中文界面、会话生命周期、审批卡、RCA/Trace 投影和 SSE gap 恢复具有完整自动化测试。已安装的候选运行时 `2b26de4`（PR #23 最终压缩合并为 `7479752`）目标控制台又通过真实 Chrome 系统遍历：Console、Overview、Tool、Safety、RCA、Audit、Allowlist、LLM 八个视图全部到达，39 个网络响应均为 HTTP 200，无浏览器错误、异常、失败请求、横向溢出或未命名 DOM/Accessibility 交互控件，因此 UI 提升为 `TARGET_VERIFIED`。

## 安全设计

- 不存在 `shell.execute`、`terminal.run`、`command.execute` 或任意命令执行接口。
- Registry 只能启动开发者在 Manifest 中声明的二进制与独立参数，不使用 `sh -c`/`bash -c`。
- 39 个 MCP Tool 全部只读；没有写 MCP Tool，更没有任意命令 Tool。写动作只能由 Agent 生成结构化 Proposal，经审批后跨 Unix Socket 到固定执行器 Handler。
- 模型提出的固定动作必须同时通过显式用户动作意图、相关成功 MCP 结果中的精确结构化服务或 PID/start-ticks 身份、本地 Policy、目标快照与人工审批；错误文本或无关 Tool 中出现目标字符串不能授权动作。
- 本地 `policies/tools.yaml` 是 Tool effect/base risk/approval/reversibility 的 Source of Truth；未知 Tool 与任意命令能力默认拒绝。
- `safeops-privexec` 只监听 Unix Socket，校验签名/期限/nonce/策略/意图/审批/范围/目标快照并仅注册固定 Handler；默认 dry-run，显式 Lab 模式只提供 allowlist 文件新建、可恢复删除、隔离/恢复、服务重启和 SIGTERM 进程终止。
- 文件新建要求目标不存在且父目录快照未变化，固定 0600 权限并限制内容大小；文件删除使用同文件系统 quarantine 原子 rename、持久化清单和身份校验，因此可恢复。永久清理没有 Handler，L3 策略默认拒绝。
- Tool 自报 risk/annotation 不是安全依据；本地 Policy 才是 Source of Truth。
- Trace 保存结构化决策摘要，不保存模型隐藏 Chain-of-Thought。

完整不变量见 [SPEC.md](SPEC.md)、[ADR.md](ADR.md) 和 [AGENTS.md](AGENTS.md)。

## LoongArch / 银河麒麟状态

- 当前 16 个 Go commands 已用 `CGO_ENABLED=0 GOOS=linux GOARCH=loong64` 成功交叉构建。
- `targetctl probe/test/report/doctor` 与 `scripts/probe-target.sh`、`scripts/target-test.sh` 已实现。Probe 覆盖 OS、架构、内核、glibc、systemd、journalctl/systemctl、ss/ip、Git/Go/GCC、proc、内存、磁盘和 journal JSON。
- 最终 `1a10880` 包已在官方 Kylin Linux Advanced Server V11 (Swan25) `loong64` VM 原生运行。Probe/Test/Doctor 报告 ID 分别为 `target_0f70b837d6ab5d7c72bd`、`target_3ca1769f7eb00323a1d8`、`target_d05c9711293c083a26c2`，三份 SHA-256 均通过审计。
- 候选运行时 `b5383e9`（PR #21 最终压缩合并为 `816f8cf`）修复了配置双 allowlist 根参数的 YAML 边界，并把 `targetctl test` 扩展为全部 39 个 Tool 的唯一调用计划；归档 SHA-256 为 `380660f64e08936f3f0581f94400bf3cedc3b51916ec21ea82cf706971b32076`，安装后报告 `target_ae6d4bbeb9ae7b8e5764` 的 39 个逐工具检查全部 `PASS`。
- 候选运行时 `2b26de4`（PR #23 最终压缩合并为 `7479752`）为安装包加入显式 SVG favicon；归档 SHA-256 为 `0accff7af8ad7eaecabe4e262f4cbc1fa6caa9359f34615e5927174af8d135f8`。安装后真实 Chrome 八视图审计的 JSON SHA-256 为 `0e3d98cf68c694a58300e8c459b5eeb9bbfd796be0d8c0734a09de303d831101`。
- 精确合并版 `7479752` 的归档 SHA-256 为 `6b2aa3220af27ac24a5c407be9df23ab82e203a878913d4a414868dc76d910a8`。安装后报告 `target_7a42c5e387e7abeea4f8` 再次确认 8/8 MCP 与 39/39 原生调用；同一安装上的六套 Benchmark 全部 `PASS`、16 项指标均已测量。
- 候选运行时 `053fc2c`（PR #27 最终压缩合并为 `4861dcf`）的归档 SHA-256 为 `d342dd4ed374cfe1b0f2145dd55dbf7eb8ce9c38e77a901a4b102e554a574ef8`。安装后报告 `target_df68d477155fd8d55d75` 以非 root `safeops` 执行 7/7 Collector、Prometheus/OpenTelemetry 两个适配模型和 39/39 MCP 调用；全部运行检查 `PASS`，整体仅因可选 `git`/`go` 缺失保持 `WARN`。
- 精确合并版 `666df77` 的归档 SHA-256 为 `33faf74629334318a15f63726baae233455206ab5379a0d7337406300a8b674f`。安装后报告 `target_4a368ebd750533d7ddb6` 再次确认 7/7 Collector、两个适配模型、8/8 MCP 与 39/39 原生调用；该回归不提升尚未执行目标正/负流程的 M17 状态。
- 原生检查确认 glibc 2.38、systemd 255、Go 1.26.4/CGO0、8/8 MCP healthy、39/39 Tool discovered/called。仅目标镜像缺少非运行时依赖的 `git`、`go` 命令，报告因此保持 `WARN`。
- 生成报告仍明确 `target_verified=false`，防止机器自提升；维护者依据报告、发布哈希与 Task/Trace 联合审计后，将对应项目能力标记为 `TARGET_VERIFIED`。

验证采用物理机发布包经反向 SSH 送入 VM 的流程；项目源代码仍以物理机当前 Git 仓库为准。完整审计见 [Kylin V11 LoongArch64 Target Verification Audit](docs/target-verification-2026-07-18.md)。

## 部署与卸载

本地 release 门禁已生成：

```text
dist/release/safeops-agent-linux-loong64.tar.gz
dist/release/safeops-agent-linux-loong64.tar.gz.sha256
```

包内含 16 个静态 LoongArch ELF、预构建中文 Web、绝对路径 MCP Manifest、Policy、Knowledge、hardened systemd units、`VERSION` 和逐文件 `SHA256SUMS`。`deploy/install.sh` 检查 root、Linux/LoongArch64、systemd、必要工具与双层哈希，创建非 root `safeops` 用户及规定目录，安装全部内容、以 `/opt/safeops/VERSION` 原子发布已校验的精确版本身份、设置权限、启动两个核心服务并轮询 `/healthz`。执行器默认 `dry-run`；仅可用 `SAFEOPS_EXECUTOR_MODE=lab` 显式开启固定 Lab Handler。四个异常复现 unit 只安装，不自动启用。

`deploy/uninstall.sh` 停止/移除服务和程序但默认保留 `/var/lib/safeops`；只有显式 `--purge-data` 才删除持久 Session、Task、审批、Trace、隔离与 Lab 数据。官方目标机默认卸载已确认 `/opt`、`/etc` 和六个 unit 被移除，而 140 个持久文件哈希及 153 条元数据完全不变；root-only 恢复 `safeops.env` 与 `privexec.hmac` 后，重装保持了 LLM、审批签名和历史 Trace 连续性。具体操作见 [部署文档](deploy/README.md)。

## 测试与 Benchmark

已通过：

- Go 单元/集成测试与 `go vet`
- 七类 Collector fixture/边界测试与真实 Ubuntu `/proc`、网络、磁盘、sysctl 统一批次 smoke
- 官方 Kylin V11/LoongArch64 上以非 root `safeops` 运行的 7/7 Collector 与两个计数型适配模型；报告不持久化 Observation 值或配置哈希
- MCP in-memory protocol 与编译后 stdio subprocess 测试
- 官方 Kylin V11 上已安装的 8 个 stdio Server、39/39 Tool 逐一结构化调用
- 官方 Kylin V11/LoongArch64 上以非 root `safeops` 运行的六套 Benchmark 与 16 项固定指标
- Trace 并发、完整生命周期、递归脱敏、崩溃尾部恢复和修改/删除/重排检测
- Web 八视图组件、危险 Markdown 转义、严重无障碍问题、TypeScript lint、production build，以及已安装目标控制台的真实 Chrome 八视图遍历
- Typed SSE 单调 ID、重复抑制、近期回放以及截断/重启 gap + 持久 Task/Trace 同步
- linux/amd64 与 linux/loong64 全 command 构建
- 真实 HTTP/SSE 纵切片和服务重启恢复

`safeops-bench` 支持 `intent`、`tool-selection`、`safety`、`rca`、`continuity`、`performance` 和便捷的 `all`。每次生成：

```text
artifacts/benchmark/benchmark-report.json
artifacts/benchmark/benchmark-report.md
```

Ubuntu/amd64 与官方 Kylin V11/loong64 报告均包含六个 PASS 套件和 16 项实测指标；每项保存样本数和方法。目标报告由已安装的 `safeops-bench` 以非 root `safeops` 身份运行，JSON SHA-256 为 `03538c50a122b44a71f2e6adcc651a104a32a2fcc0e124afba703a8813a9dc24`。这些数字仍是受控 fixture 对生产算法、持久化和执行边界的本机评测，不能外推为真实世界准确率；延迟只代表该目标环境。单独运行套件时，未选择指标保持 `NOT_MEASURED`。

## 当前限制

- 七类 Collector 已覆盖题目指定的 proc/process、磁盘、网络、systemd、journal、系统配置和 allowlist 配置变化，并已在官方目标机原生执行；Prometheus/OTel 传输无关适配模型也已原生完成计数审计，但尚未部署或验证外部遥测平台。
- 已有 39 个只读 Tool 覆盖八个要求域；Registry 生命周期、工具集变化、非重叠周期健康、依赖元数据、真实 initialize 身份及有界失败/恢复历史已通过 Ubuntu 协议和 API/UI 测试，新增周期行为仍待本次精确构建在官方 Kylin 目标机验证。
- 通用 Agent Runtime 已用真实兼容 Provider 在目标机验证；近期用户/助手消息和有序已选资源会以有界、脱敏结构进入追问规划，单次 Provider 调用受持久 Agent 截止时间约束。无 Provider 时仍可使用 CPU/内存确定性纵切片。
- 模型申请固定服务重启/进程终止，以及读写根分离的路径资源管理器当前为 `TESTED`；尚未在官方 Kylin 目标机完成这一新增范围的原生审批执行与真实浏览器复测，不能沿用旧版本证据提升为 `TARGET_VERIFIED`。
- SSE 保留最近 200 个进程内事件用于 `Last-Event-ID` 回放；跨重启不伪造历史，而是发出 `task.gap` + 持久 Task 快照并让前端回读完整 Trace。
- Session JSON 当前内嵌 Messages；文件锁与原子 mutation 已避免并发丢失，但超长会话未来仍可拆成独立 Message Store。
- Guard/Risk、完整 Trace、审批恢复、受限 Lab Executor、Rollback、Evidence Graph/BM25/RCA、三个固定恢复状态机、多轮文件、八视图 Web、六套 Benchmark、七类 Collector、两个遥测适配模型、targetctl 和 release 安装/健康/数据保留卸载均已取得对应范围的目标证据。

## 项目文档

- [贡献指南](CONTRIBUTING.md)
- [安全报告策略](SECURITY.md)
- [规格](SPEC.md)
- [里程碑计划](PLAN.md)
- [实现约束](IMPLEMENT.md)
- [真实状态与验证](STATUS.md)
- [目标证据索引](docs/evidence/README.md)
- [架构决策](ADR.md)
- [架构差距 v3](docs/architecture-gap-v3.md)
- [比赛要求覆盖矩阵](docs/competition-requirements-matrix.md)
- [MCP 能力矩阵](docs/mcp-capability-matrix.md)
- [2026-07-18 Kylin V11/LoongArch64 目标验证审计](docs/target-verification-2026-07-18.md)
- [参考项目研究总结](docs/research/SUMMARY.md)
