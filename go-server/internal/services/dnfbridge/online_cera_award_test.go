package dnfbridge

import (
	"context"
	"testing"

	dnfcerashop "longheng.io/server/internal/modules/dnf/cerashop"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/repository/memory"
)

func onlineCeraFixture(t *testing.T, accounts ...string) (*Service, dnfrepo.Group) {
	t.Helper()
	ctx := context.Background()
	group := memory.NewMemoryGroup()
	for _, id := range accounts {
		if err := group.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: id, Metadata: map[string]string{}}); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{
		options:            options{},
		repositoryProvider: func() (dnfrepo.Group, bool) { return group, true },
		gameSessions:       map[uint16]*gameSession{},
	}
	return service, group
}

func TestAwardOnlineCeraCreditsAccountOncePerTick(t *testing.T) {
	ctx := context.Background()
	service, group := onlineCeraFixture(t, "owner")
	// 同一账号两个角色同时在线：点券是账号共享的，只能发一份。
	service.gameSessions[19] = &gameSession{connID: "a", accountID: "owner", accountFromToken: true, selectedCharacterID: 19}
	service.gameSessions[20] = &gameSession{connID: "b", accountID: "owner", accountFromToken: true, selectedCharacterID: 20}

	if awarded := service.awardOnlineCeraOnce(ctx, 100); awarded != 1 {
		t.Fatalf("awarded=%d, want 1 account", awarded)
	}
	account, _, _ := group.Account.Load(ctx, "owner")
	if got := dnfcerashop.Balance(account); got != 100 {
		t.Fatalf("balance=%d, want 100", got)
	}
	if awarded := service.awardOnlineCeraOnce(ctx, 100); awarded != 1 {
		t.Fatalf("second tick awarded=%d, want 1", awarded)
	}
	account, _, _ = group.Account.Load(ctx, "owner")
	if got := dnfcerashop.Balance(account); got != 200 {
		t.Fatalf("balance after two ticks=%d, want 200", got)
	}
}

func TestAwardOnlineCeraSkipsSessionsWithoutAccount(t *testing.T) {
	ctx := context.Background()
	service, group := onlineCeraFixture(t, "owner")
	service.gameSessions[19] = &gameSession{connID: "no-account", selectedCharacterID: 19}
	service.gameSessions[20] = &gameSession{connID: "zero-char", accountID: "owner"}

	if awarded := service.awardOnlineCeraOnce(ctx, 100); awarded != 1 {
		t.Fatalf("awarded=%d, want 1", awarded)
	}
	account, _, _ := group.Account.Load(ctx, "owner")
	if got := dnfcerashop.Balance(account); got != 100 {
		t.Fatalf("balance=%d, want 100", got)
	}
}

func TestAwardOnlineCeraDisabledWhenAmountIsZero(t *testing.T) {
	ctx := context.Background()
	service, group := onlineCeraFixture(t, "owner")
	service.gameSessions[19] = &gameSession{connID: "a", accountID: "owner", accountFromToken: true, selectedCharacterID: 19}

	if awarded := service.awardOnlineCeraOnce(ctx, 0); awarded != 0 {
		t.Fatalf("disabled award returned %d", awarded)
	}
	account, _, _ := group.Account.Load(ctx, "owner")
	if got := dnfcerashop.Balance(account); got != 0 {
		t.Fatalf("disabled award changed balance to %d", got)
	}
}
