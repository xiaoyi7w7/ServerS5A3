---
category: 群服内容
---

# 在线角色每分钟发放点券

## 变更

- 新增 `DNFBRIDGE_ONLINE_CERA_PER_MINUTE`（默认 0，关闭）：大于 0 时，服务每分钟给每个在线角色所属账号发放该数量的点券。
- 点券是账号共享余额（`dnf_accounts.metadata_json["account_cera"]`），同一账号多个角色同时在线只发放一份，避免按角色数翻倍。
- 发放入账只写 `account_cera` 一项元数据，不覆盖同账号其它元数据；入账后通过会话事件队列向客户端推送权威钱包，界面即时刷新。
- 单个账号入账失败不影响其它在线角色，下个周期自然重试；金额为 0 时不建协程，未配置该环境变量的部署行为与上游一致。
- 本项为群服定制功能，只存在于私有分支，不面向公开主干。

## 验证

- `internal/modules/dnf/cerashop/credit_test.go`：入账并保留其它元数据、非正额度只读、账号不存在、溢出前拒绝写入。
- `internal/services/dnfbridge/online_cera_award_test.go`：同账号两角色只发一份、两轮累加、无账号/无角色会话跳过、金额为 0 不发放。
- 本地 `go build ./...` 与上述用例通过；`test_core.sh` 中 `TestP05L1CompletedReturnAppearanceChangeRejectsLateMode8V2` 失败，已确认在改动前的 `578221d` 通过、在上游 `6c4b6ae4` 起失败，属于既有回归，与本项无关。未部署到生产。
