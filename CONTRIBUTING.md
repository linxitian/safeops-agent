# 为 SafeOps Agent 贡献

感谢参与 SafeOps Agent。这个项目处理系统证据、审批、特权边界和可审计执行，因此可维护性与安全约束同等重要。

## 开始之前

- 小型缺陷可以直接提交 PR；新能力、协议变化和权限边界变化请先创建 Issue。
- 安全漏洞请使用 GitHub Security Advisory 私密报告，不要创建公开 Issue。
- 不要提交 API Key、Token、HMAC 密钥、真实生产日志、个人数据或模型隐藏思维过程。

## 开发环境

需要 Linux、Go 1.25+、Node.js 24 和 npm。除非有记录完整的目标兼容性决策，否则保持：

```bash
export CGO_ENABLED=0
```

首次安装前端依赖：

```bash
npm --prefix web ci
```

## 分支和提交

1. 从最新 `main` 创建短生命周期分支，例如 `feature/...`、`fix/...`、`docs/...` 或 `agent/...`。
2. 保持一次 PR 只解决一个明确问题，不混入无关格式化或重构。
3. 提交标题使用简短的祈使式描述；在 PR 正文说明原因、影响、风险和验证证据。
4. 通过 CI 和至少一名代码所有者审查后再合并。

## 必须保持的产品边界

- 不得添加任意命令或 Shell 执行工具，也不得调用 `sh -c` 或 `bash -c`。
- MCP Server 和 `safeops-server` 不以 root 运行；特权写操作必须经过 Unix Socket 到 `safeops-privexec` 的固定处理器。
- MCP 工具提供的风险提示不可信，本地策略才是权威；未知写工具必须 fail closed。
- 不持久化或显示模型隐藏思维过程，只保留结构化决策摘要、假设、证据、工具选择、Guard 结果和完成评估。
- LoongArch64 交叉构建不等于银河麒麟 V11 目标机验证。

## 本地验证

核心改动至少运行：

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go vet ./...
npm --prefix web test
npm --prefix web run lint
npm --prefix web run build
./scripts/check-cross-build.sh amd64
./scripts/check-cross-build.sh loong64
```

MCP 行为必须使用官方 SDK transport 在真实协议层验证，直接调用 Handler 不能作为协议证据。持久状态必须先写入，再发布 SSE 事件。

## 状态和文档

状态只能使用 `NOT_STARTED`、`PARTIAL`、`IMPLEMENTED`、`TESTED` 或 `TARGET_VERIFIED`。实现存在但缺少相关测试时不能写成 `TESTED`；没有官方目标机报告时不能写成 `TARGET_VERIFIED`。

里程碑状态变化时，同步更新：

- `STATUS.md`
- `docs/competition-requirements-matrix.md`
- `docs/mcp-capability-matrix.md`
- README 中的功能数量和状态说明

## PR 合并标准

PR 应当说明用户/运维影响、失败和回滚路径，并附上实际执行的验证命令。涉及策略、审批、Executor、Trace、持久化或部署的变更，需要相应的负向测试与证据。
