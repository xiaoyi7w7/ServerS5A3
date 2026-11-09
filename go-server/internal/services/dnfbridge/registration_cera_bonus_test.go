package dnfbridge

import (
	"context"
	"testing"
	"time"

	dnfcerashop "longheng.io/server/internal/modules/dnf/cerashop"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestRegistrationCeraWindowUsesBeijingTime(t *testing.T) {
	start, end, err := registrationCeraWindow("2026-11-06", "2026-11-07")
	if err != nil {
		t.Fatal(err)
	}
	if got := start.UTC().Format("2006-01-02 15:04:05"); got != "2026-11-05 16:00:00" {
		t.Fatalf("窗口起点 UTC = %s, want 2026-11-05 16:00:00", got)
	}
	if got := end.UTC().Format("2006-01-02 15:04:05"); got != "2026-11-07 16:00:00" {
		t.Fatalf("窗口终点 UTC = %s, want 2026-11-07 16:00:00", got)
	}
}

func TestRegistrationCeraWindowRejectsBadConfig(t *testing.T) {
	for _, tc := range []struct{ from, to string }{
		{"", "2026-11-07"},
		{"2026-11-06", "not-a-date"},
		{"2026-11-08", "2026-11-07"},
	} {
		if _, _, err := registrationCeraWindow(tc.from, tc.to); err == nil {
			t.Fatalf("应拒绝 %q..%q", tc.from, tc.to)
		}
	}
}

func registrationCeraFixture(t *testing.T, characters ...uint16) (*Service, dnfrepo.Group) {
	t.Helper()
	ctx := context.Background()
	group := memory.NewMemoryGroup()
	if err := group.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "owner", Metadata: map[string]string{},
	}); err != nil {
		t.Fatal(err)
	}
	sessions := make(map[uint16]*gameSession, len(characters))
	for _, id := range characters {
		sessions[id] = &gameSession{
			connID: "c", accountID: "owner", accountFromToken: true,
			selectedCharacterID: id,
		}
	}
	service := &Service{
		options:            options{},
		repositoryProvider: func() (dnfrepo.Group, bool) { return group, true },
		gameSessions:       sessions,
	}
	return service, group
}

func registrationCeraAlwaysEligible(context.Context, string, time.Time, time.Time) (bool, error) {
	return true, nil
}

// registrationCeraLiveWindow 返回一个"当前有效"的发放窗口。
// 发放函数用 time.Now() 判定窗口是否结束，如果把日期写死成具体某两天，
// 用例会随真实时间推移自动失效（2026-11-09 起 11-06..11-07 的窗口已过期）。
func registrationCeraLiveWindow() (time.Time, time.Time) {
	now := time.Now()
	return now.Add(-time.Hour), now.Add(time.Hour)
}

func TestRegistrationCeraGrantsOnceAndKeepsReceipt(t *testing.T) {
	ctx := context.Background()
	service, group := registrationCeraFixture(t, 19)
	start, end := registrationCeraLiveWindow()

	if got := service.grantRegistrationCeraOnce(ctx, 20000, start, end, registrationCeraAlwaysEligible); got != 1 {
		t.Fatalf("首次发放账号数 = %d, want 1", got)
	}
	account, found, err := group.Account.Load(ctx, "owner")
	if err != nil || !found {
		t.Fatalf("账号读取失败: found=%v err=%v", found, err)
	}
	if got := dnfcerashop.Balance(account); got != 20000 {
		t.Fatalf("余额 = %d, want 20000", got)
	}
	if account.Metadata[registrationCeraReceiptKey] != registrationCeraReceiptSet {
		t.Fatalf("回执未写入，且余额可能被覆盖: metadata=%+v", account.Metadata)
	}
	if got := service.grantRegistrationCeraOnce(ctx, 20000, start, end, registrationCeraAlwaysEligible); got != 0 {
		t.Fatalf("第二周期不应重复发放, got %d", got)
	}
	account, _, _ = group.Account.Load(ctx, "owner")
	if got := dnfcerashop.Balance(account); got != 20000 {
		t.Fatalf("重复发放导致余额 = %d, want 20000", got)
	}
}

func TestRegistrationCeraSkipsIneligibleAccounts(t *testing.T) {
	ctx := context.Background()
	service, group := registrationCeraFixture(t, 19)
	start, end := registrationCeraLiveWindow()
	never := func(context.Context, string, time.Time, time.Time) (bool, error) { return false, nil }

	if got := service.grantRegistrationCeraOnce(ctx, 20000, start, end, never); got != 0 {
		t.Fatalf("窗口外账号不应发放, got %d", got)
	}
	account, _, _ := group.Account.Load(ctx, "owner")
	if got := dnfcerashop.Balance(account); got != 0 {
		t.Fatalf("余额应仍为 0, got %d", got)
	}
}

func TestRegistrationCeraDedupesMultipleCharacters(t *testing.T) {
	ctx := context.Background()
	service, group := registrationCeraFixture(t, 19, 20)
	start, end := registrationCeraLiveWindow()

	if got := service.grantRegistrationCeraOnce(ctx, 20000, start, end, registrationCeraAlwaysEligible); got != 1 {
		t.Fatalf("同账号两角色应只发一份, got %d", got)
	}
	account, _, _ := group.Account.Load(ctx, "owner")
	if got := dnfcerashop.Balance(account); got != 20000 {
		t.Fatalf("余额 = %d, want 20000", got)
	}
}

func TestRegistrationCeraStopsAfterWindowEnds(t *testing.T) {
	ctx := context.Background()
	service, group := registrationCeraFixture(t, 19)
	start, end, _ := registrationCeraWindow("2020-01-01", "2020-01-02")

	if got := service.grantRegistrationCeraOnce(ctx, 20000, start, end, registrationCeraAlwaysEligible); got != 0 {
		t.Fatalf("已结束的活动不应发放, got %d", got)
	}
	account, _, _ := group.Account.Load(ctx, "owner")
	if got := dnfcerashop.Balance(account); got != 0 {
		t.Fatalf("余额应仍为 0, got %d", got)
	}
}
