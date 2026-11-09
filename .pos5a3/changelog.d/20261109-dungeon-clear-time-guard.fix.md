---
category: 群服内容
---

# 副本通关时长下限拦截（速通中止结算并断开连接）

## 变更

- 新增 `DNFBRIDGE_DUNGEON_MIN_CLEAR_SECONDS`（默认 0，关闭）：大于 0 时，结算前比较本次通关时长，低于该秒数则中止结算并断开该会话。
- 判定基准全部来自服务端时钟：起点 `runtime.startedAt`（进图时写入）、终点 `runtime.clearMapCompletionAt`（服务端判定通关时写入，缺失时回落到 `settlementEntryAt`）。客户端上报的任何数值都不参与判定。
- 组队时以队伍最早进入时刻（`partyInstance.createdAt`）为起点，而不是成员各自的 `startedAt`：中途加入或断线重进的成员起点偏晚，用其自身起点会算出偏短的时长而被误判。
- 拦截点在 `produceCurrentDungeonSettlementPlanWithResourcesLocked` 内、`commitCurrentDungeonSettlementWithPotion` 之前，因此经验、金币、掉落、翻牌一律不落库；命中后关闭连接，玩家退回登录界面，可立即重登，本局收益作废，不属于封禁。
- 命中留证日志 `game-dungeon-clear-time-too-short-kicked`，字段 `char_id`、`dungeon_id`、`difficulty`、`clear_ms`、`min_seconds`、`completion_key`、`party_size`、`reward_assets=not_committed`。
- 开关为 0 时不执行任何额外判断，行为与上游一致；配置非法（负数）同样视为关闭。

## 验证

- `internal/services/dnfbridge/dungeon_clear_time_guard_test.go`：阈值边界（29999ms 拦截、30000ms 放行）、开关关闭与负阈值不拦截、单人取进图时刻、组队取更早的建队时刻、建队时刻更晚时不采用、通关时刻缺失或时间倒序时不判定、回落 `settlementEntryAt`、组队人数日志字段。
- 本地 `go build ./...`、`go vet ./internal/services/dnfbridge/` 通过；新增用例通过。
