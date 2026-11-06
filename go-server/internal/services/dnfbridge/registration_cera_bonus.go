package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dnfcerashop "longheng.io/server/internal/modules/dnf/cerashop"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// 本服定制：限时注册赠送点券。与 online_cera_award.go 共用同一个「每分钟」
// 结算周期，但职责独立——泡点关掉时注册赠送仍然生效。
const (
	registrationCeraReceiptKey = "account_cera_registration_bonus_v1"
	registrationCeraReceiptSet = "granted"
	registrationCeraInterval   = time.Minute
)

var errRegistrationCeraRegistryUnavailable = errors.New("dnf account registry unavailable")

// registrationCeraLocation 固定用北京时间解释配置里的日期，避免容器 UTC
// 与玩家所在时区不一致时把「今天」算错。
var registrationCeraLocation = time.FixedZone("CST", 8*3600)

// registrationCeraWindow 把 "2026-11-06" / "2026-11-07" 解析成
// 左闭右开的北京时间段 [11-06 00:00, 11-08 00:00)。
func registrationCeraWindow(from, to string) (time.Time, time.Time, error) {
	start, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(from), registrationCeraLocation)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start date %q: %w", from, err)
	}
	last, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(to), registrationCeraLocation)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end date %q: %w", to, err)
	}
	end := last.AddDate(0, 0, 1)
	if !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("empty registration window %q..%q", from, to)
	}
	return start, end, nil
}

func (s *Service) startRegistrationCeraBonus(ctx context.Context) {
	if s == nil || s.options.registrationCeraAmount <= 0 {
		return
	}
	start, end, err := registrationCeraWindow(s.options.registrationCeraFrom, s.options.registrationCeraTo)
	if err != nil {
		// 配置写错时宁可不发，也别把点券发给所有账号。
		s.logWarn("registration cera bonus disabled", "error", err)
		return
	}
	s.logInfo("registration cera bonus enabled",
		"amount", s.options.registrationCeraAmount,
		"from", s.options.registrationCeraFrom,
		"to", s.options.registrationCeraTo,
		"expires_at", end.Format(time.RFC3339))
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(registrationCeraInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !time.Now().Before(end) {
					return // 活动结束，连空转都停掉
				}
				s.grantRegistrationCeraOnce(ctx, int64(s.options.registrationCeraAmount), start, end, s.registrationCeraEligible)
			}
		}
	}()
}

// registrationCeraEligible 直接查登录器注册表的 created_at。
// 库里 datetime 是无时区的 UTC（容器 MySQL 为 UTC），所以把北京时间窗口
// 换算成 UTC 后以字符串下推比较，绕开驱动的 loc 解析歧义。
func (s *Service) registrationCeraEligible(ctx context.Context, accountID string, start, end time.Time) (bool, error) {
	api := s.accountAPI
	if api == nil || api.db == nil || strings.TrimSpace(api.table) == "" {
		return false, errRegistrationCeraRegistryUnavailable
	}
	var matched int
	err := api.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM "+api.table+
			" WHERE BINARY account_id = BINARY ? AND created_at >= ? AND created_at < ?",
		accountID,
		start.UTC().Format("2006-01-02 15:04:05"),
		end.UTC().Format("2006-01-02 15:04:05"),
	).Scan(&matched)
	if err != nil {
		return false, err
	}
	return matched > 0, nil
}

// grantRegistrationCeraOnce 对当前在线、注册日期落在窗口内、且从未领过的
// 账号一次性入账。返回本次成功发放的账号数。
//
// 幂等靠 dnf_account_metadata 里的永久回执；加款与写回执分两步，若中间
// 进程异常退出，最坏情况是该账号下周期重复入账一次，日志会留下记录。
func (s *Service) grantRegistrationCeraOnce(
	ctx context.Context,
	amount int64,
	start, end time.Time,
	eligible func(context.Context, string, time.Time, time.Time) (bool, error),
) int {
	if s == nil || amount <= 0 || eligible == nil {
		return 0
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Account == nil {
		return 0
	}
	if ctx == nil {
		ctx = context.Background()
	}

	granted := 0
	for _, target := range s.onlineAccountTargets() {
		if !time.Now().Before(end) {
			break
		}
		entry := target
		account, found, err := repositories.Account.Load(ctx, entry.accountID)
		if err != nil || !found {
			if err != nil {
				s.logWarn("registration cera load failed", "account_id", entry.accountID, "error", err)
			}
			continue
		}
		if account.Metadata[registrationCeraReceiptKey] == registrationCeraReceiptSet {
			continue // 永久回执：换角色、重登、同账号多开都不会再发
		}
		ok, err := eligible(ctx, entry.accountID, start, end)
		if err != nil {
			s.logWarn("registration cera eligibility check failed", "account_id", entry.accountID, "error", err)
			continue
		}
		if !ok {
			continue
		}
		if _, _, err := dnfcerashop.CreditBalance(ctx, repositories.Account, entry.accountID, amount); err != nil {
			s.logWarn("registration cera credit failed", "account_id", entry.accountID, "error", err)
			continue
		}
		// 必须重新加载：CreditBalance 刚写过 account_cera，用旧快照写回执
		// 会在非原生单键写入的仓储上把余额覆盖回去。
		fresh, found, err := repositories.Account.Load(ctx, entry.accountID)
		if err != nil || !found {
			s.logWarn("registration cera receipt reload failed", "account_id", entry.accountID, "error", err)
			continue
		}
		if err := dnfrepo.SaveAccountMetadataEntry(
			ctx, repositories.Account, fresh,
			registrationCeraReceiptKey, registrationCeraReceiptSet, time.Now(),
		); err != nil {
			s.logWarn("registration cera receipt failed", "account_id", entry.accountID, "error", err)
			continue
		}
		granted++
		s.logInfo("registration cera granted", "account_id", entry.accountID, "amount", amount)
		if entry.session.conn == nil {
			continue
		}
		_ = s.postGameSessionCharacterEvent(
			entry.session, "registration-cera-bonus",
			entry.identity.character, entry.identity.generation,
			func() error {
				if !s.boundGameSessionCharacterCurrent(entry.identity) {
					return nil
				}
				return s.sendCurrentAccountWallet(entry.session)
			},
		)
	}
	if granted > 0 {
		s.logInfo("registration cera bonus settled", "accounts", granted, "amount_each", amount)
	}
	return granted
}
