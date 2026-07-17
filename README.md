# SafeOps Agent

[![CI](https://github.com/linxitian/safeops-agent/actions/workflows/ci.yml/badge.svg)](https://github.com/linxitian/safeops-agent/actions/workflows/ci.yml)

面向银河麒麟操作系统的安全自治智能运维智能体，中国软件杯 A2 赛题项目。

SafeOps 是自然语言与 Linux 运维能力之间的安全桥梁：它通过真实系统证据和 MCP 工具完成调查，并把受控写操作约束在“意图校验、风险评估、审批、最小权限执行、验证与回滚”的边界内。它不是普通聊天机器人，也不会把模型生成的 Shell 命令直接交给 Linux。

> 当前状态：已在 Ubuntu 完成 CPU/内存真实纵切片；七类统一 Collector、8 域 39 个 MCP Tool、双 Guard/Risk、完整 Trace、通用 Agent Runtime、证据图/BM25/RCA、审批自动恢复、最小权限 Lab 执行、受控文件新建/可恢复删除和六页面中文 Web 均有本地测试。端口、CPU、磁盘/日志三个受控恢复状态机已通过后端验证，但尚未完成安装后的 live Demo。`targetctl`、六套件 Benchmark 和 M16 LoongArch64 发布包已实现并通过本地门禁；尚未在银河麒麟 V11 官方虚拟机安装与验证。

## 五大核心支柱

1. **OS 环境深度感知**：统一 Platform/Collector/Observation，采集真实 Linux 状态。
2. **MCP 运维插件化**：官方 MCP Go SDK、Manifest Registry、协议发现与健康检查。
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

Static/Intent Guard 与 Risk 已接入只读和写动作准备链。Approval、ActionEnvelope、`safeops-privexec`、Verification、审批自动恢复和原子文件回滚已有集成测试；真实写仅存在于显式 `lab` 模式的固定 Handler，包含受控文件新建、可恢复删除、隔离/恢复、服务重启和进程 SIGTERM，且未作为 MCP Tool 暴露。主端口冲突以及 CPU、磁盘/日志受控处置计划均有持久化跨审批状态机测试；安装后的真实 systemd/API/UI 闭环仍待验证。详见 [SPEC.md](SPEC.md) 和 [PLAN.md](PLAN.md)。

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
- Node.js 与 npm
- 推荐 `make`；若缺失，可直接运行 Makefile 中的底层命令
- `systemctl`、`journalctl`、`ss`、`ip` 用于固定参数感知与目标探测

当前开发机最初缺少 Go；本轮从 Go 官方下载并校验 SHA-256 后在用户目录安装了 Go 1.26.4。此安装不属于项目发布包。

## 快速开始

```bash
npm --prefix web install
make test
make build-native
./bin/safeops-server \
  -listen 127.0.0.1:8080 \
  -data ./data \
  -mcp-config ./config/mcp_servers.yaml
```

另一个终端启动开发前端：

```bash
npm --prefix web run dev
```

打开 `http://127.0.0.1:5173`，输入：

```text
查看 CPU 和内存。
```

未配置 LLM 时，Agent 提供确定性的 CPU/内存只读纵切片。配置 Provider 后，通用 Runtime 只能选择 Registry 实际发现的 L0 Tool，并在本地校验 JSON Schema；每个 Tool Result 都重新进入 Runtime。写动作准备还需要显式提供执行器配置和 0600 HMAC secret，默认关闭。

## 配置与环境变量

MCP 注册配置为 `config/mcp_servers.yaml`。当前启用 system、process、network、journal、service、diagnostic、file、config 八个内建 Server。file/config 只能访问 Manifest 参数中的绝对 allowlist 根；配置工具不返回正文。

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

输入/输出由 typed `mcp.AddTool` 生成并校验 Schema。Registry 通过 stdio 完成 initialize、分页 tools discovery、schema/tool-set fingerprint、ping、enable/disable、rediscover 和 tools/call；Tool 集变化会保留前后指纹。详见 [MCP 能力矩阵](docs/mcp-capability-matrix.md)。

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

### 已测试但尚未形成完整比赛 Demo 的组件

- 端口状态机有固定 10 步计划：五个只读 Tool、D1/RAG、精确进程 L2 审批、独立服务重启 L1 审批，以及服务、端口、loopback HTTP 三重验证。后端自动恢复测试通过；尚未安装受控 units 做 live 运行。
- CPU 状态机持久化基线与精确进程身份，审批后重新采样 CPU 并验证进程消失；恢复幅度不足会明确失败。磁盘/日志状态机先停止固定 writer，再以新文件快照发起独立隔离审批，并明确隔离不等于释放同文件系统物理空间。两条后端测试通过；尚未做安装后的 live 运行。
- 后端测试已完成 selected resources、第三项指代、直接 allowlist 路径、审批绑定、受控新建、可恢复删除、真实隔离、下一轮恢复和上下文清理。中文界面六个要求页面、会话优先侧边栏、会话搜索/重命名/归档、精确目标审批、结果卡、RCA/Trace 投影和 SSE gap 恢复均有组件或 API 测试；多轮文件的完整 live chat 演示仍待执行。

这些场景不得在比赛材料中描述为已完成。

## 安全设计

- 不存在 `shell.execute`、`terminal.run`、`command.execute` 或任意命令执行接口。
- Registry 只能启动开发者在 Manifest 中声明的二进制与独立参数，不使用 `sh -c`/`bash -c`。
- 39 个 MCP Tool 全部只读；没有写 MCP Tool，更没有任意命令 Tool。写动作只能由 Agent 生成结构化 Proposal，经审批后跨 Unix Socket 到固定执行器 Handler。
- 本地 `policies/tools.yaml` 是 Tool effect/base risk/approval/reversibility 的 Source of Truth；未知 Tool 与任意命令能力默认拒绝。
- `safeops-privexec` 只监听 Unix Socket，校验签名/期限/nonce/策略/意图/审批/范围/目标快照并仅注册固定 Handler；默认 dry-run，显式 Lab 模式只提供 allowlist 文件新建、可恢复删除、隔离/恢复、服务重启和 SIGTERM 进程终止。
- 文件新建要求目标不存在且父目录快照未变化，固定 0600 权限并限制内容大小；文件删除使用同文件系统 quarantine 原子 rename、持久化清单和身份校验，因此可恢复。永久清理没有 Handler，L3 策略默认拒绝。
- Tool 自报 risk/annotation 不是安全依据；本地 Policy 才是 Source of Truth。
- Trace 保存结构化决策摘要，不保存模型隐藏 Chain-of-Thought。

完整不变量见 [SPEC.md](SPEC.md)、[ADR.md](ADR.md) 和 [AGENTS.md](AGENTS.md)。

## LoongArch / 银河麒麟状态

- 当前 16 个 Go commands 已用 `CGO_ENABLED=0 GOOS=linux GOARCH=loong64` 成功交叉构建。
- `targetctl probe/test/report/doctor` 与 `scripts/probe-target.sh`、`scripts/target-test.sh` 已实现。Probe 覆盖 OS、架构、内核、glibc、systemd、journalctl/systemctl、ss/ip、Git/Go/GCC、proc、内存、磁盘和 journal JSON。
- 本机 `targetctl test` 报告为 Ubuntu/amd64 `WARN`，真实 MCP 检查为 8/8 healthy、39/39 discovered；报告始终明确 `target_verified=false`。
- 尚未在官方银河麒麟 V11 VM 运行二进制、目标测试或任何 Demo。
- 因此兼容状态是“交叉构建通过，目标机尚未验证”，不是 `TARGET_VERIFIED`。

最终采用 VM 主动 `git pull` 的拉取式验证流程。只有真实目标报告可以驱动 Kylin-specific 适配。

## 部署与卸载

本地 release 门禁已生成：

```text
dist/release/safeops-agent-linux-loong64.tar.gz
dist/release/safeops-agent-linux-loong64.tar.gz.sha256
```

包内含 16 个静态 LoongArch ELF、预构建中文 Web、绝对路径 MCP Manifest、Policy、Knowledge、hardened systemd units、`VERSION` 和逐文件 `SHA256SUMS`。`deploy/install.sh` 检查 root、Linux/LoongArch64、systemd、必要工具与双层哈希，创建非 root `safeops` 用户及规定目录，安装全部内容、设置权限、启动两个核心服务并轮询 `/healthz`。执行器默认 `dry-run`；仅可用 `SAFEOPS_EXECUTOR_MODE=lab` 显式开启固定 Lab Handler。四个异常复现 unit 只安装，不自动启用。

`deploy/uninstall.sh` 停止/移除服务和程序但默认保留 `/var/lib/safeops`；只有显式 `--purge-data` 才删除持久 Session、Task、审批、Trace、隔离与 Lab 数据。脚本语法、39 项包内哈希和六个 unit 的 staged-root 校验已通过；真实 root 安装、启动、健康检查和卸载仍必须在官方目标机验证。

## 测试与 Benchmark

已通过：

- Go 单元/集成测试与 `go vet`
- 七类 Collector fixture/边界测试与真实 Ubuntu `/proc`、网络、磁盘、sysctl 统一批次 smoke
- MCP in-memory protocol 与编译后 stdio subprocess 测试
- Trace 并发、完整生命周期、递归脱敏、崩溃尾部恢复和修改/删除/重排检测
- Web 六页面组件、危险 Markdown 转义、严重无障碍问题、TypeScript lint 与 production build
- Typed SSE 单调 ID、重复抑制、近期回放以及截断/重启 gap + 持久 Task/Trace 同步
- linux/amd64 与 linux/loong64 全 command 构建
- 真实 HTTP/SSE 纵切片和服务重启恢复

`safeops-bench` 支持 `intent`、`tool-selection`、`safety`、`rca`、`continuity`、`performance` 和便捷的 `all`。每次生成：

```text
artifacts/benchmark/benchmark-report.json
artifacts/benchmark/benchmark-report.md
```

当前 Ubuntu/amd64 报告含六个 PASS 套件和 16 项实测指标；每项保存样本数和方法。样本是受控 fixture 对生产算法、持久化和执行边界的本机评测，不是目标机证据，也不能外推为真实世界准确率。单独运行套件时，未选择指标保持 `NOT_MEASURED`。

## 当前限制

- 七类 Collector 已覆盖题目指定的 proc/process、磁盘、网络、systemd、journal、系统配置和 allowlist 配置变化；Prometheus/OTel 当前只提供传输无关适配模型，未部署外部遥测平台。
- 已有 39 个只读 Tool 覆盖八个要求域，Registry 生命周期与工具集变化检测已完成 Ubuntu 测试；周期健康循环、依赖探测和版本历史仍待补齐。
- 通用 Agent Runtime 已实现，但真实 OpenAI-compatible Provider 尚未用用户凭据验证；无 Provider 时只启用 CPU/内存确定性纵切片。
- SSE 保留最近 200 个进程内事件用于 `Last-Event-ID` 回放；跨重启不伪造历史，而是发出 `task.gap` + 持久 Task 快照并让前端回读完整 Trace。
- Session JSON 当前内嵌 Messages；文件锁与原子 mutation 已避免并发丢失，但超长会话未来仍可拆成独立 Message Store。
- Guard/Risk、完整 Trace、审批恢复、受限 Lab Executor、Rollback、Evidence Graph/BM25/RCA、三个固定恢复状态机、中文管理页面、Benchmark、targetctl 和 release 产物已有本地测试；三个安装后 live 比赛 Demo 以及目标机安装/卸载尚未完成。

## 项目文档

- [贡献指南](CONTRIBUTING.md)
- [安全报告策略](SECURITY.md)
- [规格](SPEC.md)
- [里程碑计划](PLAN.md)
- [实现约束](IMPLEMENT.md)
- [真实状态与验证](STATUS.md)
- [架构决策](ADR.md)
- [架构差距 v3](docs/architecture-gap-v3.md)
- [比赛要求覆盖矩阵](docs/competition-requirements-matrix.md)
- [MCP 能力矩阵](docs/mcp-capability-matrix.md)
- [参考项目研究总结](docs/research/SUMMARY.md)
