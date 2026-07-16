## 变更摘要

<!-- 说明改了什么，以及为什么需要这项变更。 -->

## 用户和运维影响

<!-- 描述用户可见行为、兼容性、部署和回滚影响；没有则写“无”。 -->

## 安全边界检查

- [ ] 未添加任意命令或 Shell 执行能力，也未调用 `sh -c` / `bash -c`
- [ ] MCP 风险提示未被当作本地策略依据；未知写工具仍然 fail closed
- [ ] 特权写操作仍通过 `safeops-privexec` 的固定处理器和 Unix Socket 边界
- [ ] 未提交凭据、密钥、个人数据或模型隐藏思维过程
- [ ] 目标、审批、回滚、Trace 或持久化语义的变化已有相应测试

## 验证

<!-- 勾选已执行项；不适用的项目请说明原因。 -->

- [ ] `CGO_ENABLED=0 go test ./...`
- [ ] `CGO_ENABLED=0 go vet ./...`
- [ ] `npm --prefix web test`
- [ ] `npm --prefix web run lint`
- [ ] `npm --prefix web run build`
- [ ] linux/amd64 全命令构建
- [ ] linux/loong64 全命令构建
- [ ] MCP 行为通过官方 SDK transport 在真实协议层验证（如适用）

## 状态与证据

- [ ] 状态用语仅使用 `NOT_STARTED`、`PARTIAL`、`IMPLEMENTED`、`TESTED` 或 `TARGET_VERIFIED`
- [ ] 若里程碑状态变化，已同步 `STATUS.md`、两份需求矩阵和 README 功能数量
- [ ] 未把交叉构建成功描述为目标机验证
