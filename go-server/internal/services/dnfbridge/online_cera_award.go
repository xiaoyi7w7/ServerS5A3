package dnfbridge

import (
	"context"
	"strings"
	"time"

	dnfcerashop "longheng.io/server/internal/modules/dnf/cerashop"
)

// defaultOnlineCeraAwardInterval 是在线点券的发放周期。
// 需求是"每分钟 100 点券"，周期本身不开放配置，避免出现"改成 10 秒"这种
// 让余额增长不再可预期的情况；要调整节奏就改这里的常量。
const defaultOnlineCeraAwardInterval = time.Minute

// startOnlineCeraAward 每分钟给在线角色所属账号发放点券。
// 金额 <= 0 时直接 return，不建协程、零副作用——本服定制功能，
// 未配置该环境变量的部署行为与上游完全一致。
func (s *Service) startOnlineCeraAward(ctx context.Context) {
	if s == nil || s.options.onlineCeraPerMinute <= 0 {
		return
	}
	interval := defaultOnlineCeraAwardInterval
	s.logInfo("online cera award enabled", "amount_per_minute", s.options.onlineCeraPerMinute, "interval", interval.String())
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.awardOnlineCeraOnce(ctx, int64(s.options.onlineCeraPerMinute))
			}
		}
	}()
}

// onlineCeraTarget 是一次结算周期内要发放的在线角色快照。
// 快照在 s.mu 内取，真正写库和推送在锁外做，避免慢 IO 占住会话表。
type onlineCeraTarget struct {
	session   *gameSession
	identity  boundGameSessionCharacter
	accountID string
}

// awardOnlineCeraOnce 执行一次发放，返回实际入账的账号数。
//
// 点券是账号共享的（dnf_accounts.metadata_json["account_cera"]），
// 所以同一账号下多个角色同时在线时只发一份，否则会按角色数翻倍。
func (s *Service) awardOnlineCeraOnce(ctx context.Context, amount int64) int {
	if s == nil || amount <= 0 {
		return 0
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Account == nil {
		return 0
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	targets := make([]onlineCeraTarget, 0, len(s.gameSessions))
	seenAccounts := make(map[string]struct{}, len(s.gameSessions))
	for characterID, session := range s.gameSessions {
		if session == nil || characterID == 0 {
			continue
		}
		accountID := strings.TrimSpace(s.accountIDForSession(session))
		if accountID == "" {
			continue
		}
		if _, duplicate := seenAccounts[accountID]; duplicate {
			continue
		}
		seenAccounts[accountID] = struct{}{}
		targets = append(targets, onlineCeraTarget{
			session: session,
			identity: boundGameSessionCharacter{
				session:    session,
				character:  characterID,
				generation: session.characterGeneration,
			},
			accountID: accountID,
		})
	}
	s.mu.Unlock()

	awarded := 0
	for _, target := range targets {
		entry := target
		if _, found, err := dnfcerashop.CreditBalance(ctx, repositories.Account, entry.accountID, amount); err != nil || !found {
			// 单个账号失败不影响其他在线角色；下个周期会自然重试。
			// 但要留下证据，否则线上只能看到"余额不动"而没有任何线索。
			if err != nil {
				s.logWarn("online cera award failed", "account_id", entry.accountID, "error", err)
			}
			continue
		}
		awarded++
		if entry.session.conn == nil {
			continue
		}
		// 余额推送走会话事件队列：点券是权威钱包，客户端要立刻看到新数字。
		_ = s.postGameSessionCharacterEvent(
			entry.session,
			"online-cera-award",
			entry.identity.character,
			entry.identity.generation,
			func() error {
				if !s.boundGameSessionCharacterCurrent(entry.identity) {
					return nil
				}
				return s.sendCurrentAccountWallet(entry.session)
			},
		)
	}
	if awarded > 0 {
		s.logInfo("online cera awarded", "accounts", awarded, "amount_each", amount)
	}
	return awarded
}
