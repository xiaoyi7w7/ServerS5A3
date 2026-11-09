package dnfbridge

import (
	"errors"
	"time"
)

// errCurrentDungeonClearTimeTooShort 表示本次通关时长低于配置下限。
// 调用方拿到它必须放弃本次结算：经验、金币、掉落都不得落库。
//
// 判定用的两个时间戳都由服务端产生，客户端无法影响：
//   - 起点：runtime.startedAt（进图那一刻写入）
//   - 终点：runtime.clearMapCompletionAt（服务端判定通关时写入）
//
// 因此"通关太快"这个结论不依赖客户端上报的任何数值。
var errCurrentDungeonClearTimeTooShort = errors.New("current dungeon clear time below configured minimum")

// currentDungeonClearTimeIsSuspicious 是纯判定函数，便于单测覆盖。
// thresholdSeconds <= 0 表示开关关闭，恒不拦截。
// 边界取"严格小于"：恰好等于阈值放行，与"小于 30 秒才踢"的规则一致。
func currentDungeonClearTimeIsSuspicious(elapsedMS uint32, thresholdSeconds int) bool {
	if thresholdSeconds <= 0 {
		return false
	}
	return int64(elapsedMS) < int64(thresholdSeconds)*1000
}

// currentDungeonClearTimeBaseline 返回判定用起点。
//
// 组队时优先取队伍最早进入时刻（partyInstance.createdAt）：每个成员各有自己的
// runtime.startedAt，中途加入或断线重进的成员起点偏晚，用他自己的 startedAt
// 会算出偏短的时长而被误判。取队伍最早时刻只会让时长变长，属于安全方向的偏差。
func currentDungeonClearTimeBaseline(runtime *runtimeDungeonState) time.Time {
	if runtime == nil {
		return time.Time{}
	}
	startedAt := runtime.startedAt
	if runtime.partyInstance != nil {
		if createdAt := runtime.partyInstance.createdAt; !createdAt.IsZero() &&
			(startedAt.IsZero() || createdAt.Before(startedAt)) {
			return createdAt
		}
	}
	return startedAt
}

// currentDungeonClearTimeGuardElapsedMilliseconds 复算判定用时长。
// 时间戳缺失或倒序时返回 false，调用方按"无法判定"处理（不拦截）。
func currentDungeonClearTimeGuardElapsedMilliseconds(runtime *runtimeDungeonState) (uint32, bool) {
	if runtime == nil {
		return 0, false
	}
	completedAt := runtime.clearMapCompletionAt
	if completedAt.IsZero() {
		completedAt = runtime.settlementEntryAt
	}
	elapsed, err := currentDungeonSettlementElapsedMilliseconds(currentDungeonClearTimeBaseline(runtime), completedAt)
	if err != nil {
		return 0, false
	}
	return elapsed, true
}

// enforceCurrentDungeonClearTimeGuardLocked 在结算提交之前拦截异常速通。
//
// 调用点必须在奖励落库之前（produceCurrentDungeonSettlementPlanWithResourcesLocked
// 算完 presentation、调用 commitCurrentDungeonSettlementWithPotion 之前），
// 否则奖励已经发出去，断开连接也没有意义。
//
// 返回 errCurrentDungeonClearTimeTooShort 时调用方直接返回该错误，本次结算整体
// 中止；连接在这里主动关闭，玩家退回登录界面。这不是封禁，重登即可继续，
// 只是本局收益全部作废。
func (s *Service) enforceCurrentDungeonClearTimeGuardLocked(session *gameSession, runtime *runtimeDungeonState) error {
	threshold := s.options.dungeonMinClearSeconds
	if threshold <= 0 {
		return nil
	}
	elapsedMS, ok := currentDungeonClearTimeGuardElapsedMilliseconds(runtime)
	if !ok {
		return nil
	}
	if !currentDungeonClearTimeIsSuspicious(elapsedMS, threshold) {
		return nil
	}
	s.logGameEvent(session, "game-dungeon-clear-time-too-short-kicked",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"difficulty", runtime.Request.Difficulty,
		"clear_ms", elapsedMS,
		"min_seconds", threshold,
		"completion_key", runtime.clearMapCompletionKey,
		"party_size", currentDungeonGuardPartySize(runtime),
		"reward_assets", "not_committed")
	if session.conn != nil {
		_ = session.conn.Close()
	}
	return errCurrentDungeonClearTimeTooShort
}

// currentDungeonGuardPartySize 只用于日志，便于事后区分单人速通与组队速通。
// 用 runtime 上的快照值而不是 instance.memberCount()：后者会取队伍实例锁，
// 在已持有副本锁的路径上引入反向加锁顺序，日志字段不值得冒这个风险。
func currentDungeonGuardPartySize(runtime *runtimeDungeonState) int {
	if runtime == nil || runtime.entryPartyCount <= 1 {
		return 1
	}
	return runtime.entryPartyCount
}
