package dnfbridge

import "strings"

// onlineAccountTarget 是一次结算周期内的在线账号快照。
// 点券按账号共享，所以按 accountID 去重：同账号多个角色同时在线只处理一次。
type onlineAccountTarget struct {
	session   *gameSession
	identity  boundGameSessionCharacter
	accountID string
}

// onlineAccountTargets 在 s.mu 内取快照，写库和客户端推送都在锁外进行，
// 避免慢 IO 占住会话表。泡点与注册赠送共用同一份口径。
func (s *Service) onlineAccountTargets() []onlineAccountTarget {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	targets := make([]onlineAccountTarget, 0, len(s.gameSessions))
	seen := make(map[string]struct{}, len(s.gameSessions))
	for characterID, session := range s.gameSessions {
		if session == nil || characterID == 0 {
			continue
		}
		accountID := strings.TrimSpace(s.accountIDForSession(session))
		if accountID == "" {
			continue
		}
		if _, duplicate := seen[accountID]; duplicate {
			continue
		}
		seen[accountID] = struct{}{}
		targets = append(targets, onlineAccountTarget{
			session: session,
			identity: boundGameSessionCharacter{
				session:    session,
				character:  characterID,
				generation: session.characterGeneration,
			},
			accountID: accountID,
		})
	}
	return targets
}
