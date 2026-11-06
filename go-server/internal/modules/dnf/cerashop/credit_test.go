package cerashop

import (
	"context"
	"errors"
	"strconv"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func creditFixture(t *testing.T, cera int64) dnfrepo.AccountRepository {
	t.Helper()
	ctx := context.Background()
	group := dnfrepomemory.NewMemoryGroup()
	metadata := map[string]string{"unrelated": "keep"}
	if cera > 0 {
		metadata[AccountCeraMetadataKey] = strconv.FormatInt(cera, 10)
	}
	if err := group.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "acc", Metadata: metadata}); err != nil {
		t.Fatal(err)
	}
	return group.Account
}

func TestCreditBalanceAddsAndKeepsOtherMetadata(t *testing.T) {
	ctx := context.Background()
	repo := creditFixture(t, 200)

	balance, found, err := CreditBalance(ctx, repo, "acc", 100)
	if err != nil || !found {
		t.Fatalf("credit failed: found=%v err=%v", found, err)
	}
	if balance != 300 {
		t.Fatalf("balance=%d, want 300", balance)
	}
	account, _, _ := repo.Load(ctx, "acc")
	if got := Balance(account); got != 300 {
		t.Fatalf("persisted balance=%d, want 300", got)
	}
	if account.Metadata["unrelated"] != "keep" {
		t.Fatalf("unrelated metadata was dropped: %+v", account.Metadata)
	}
}

func TestCreditBalanceNonPositiveDeltaOnlyReads(t *testing.T) {
	ctx := context.Background()
	repo := creditFixture(t, 50)
	balance, found, err := CreditBalance(ctx, repo, "acc", 0)
	if err != nil || !found || balance != 50 {
		t.Fatalf("zero delta should read only: balance=%d found=%v err=%v", balance, found, err)
	}
}

func TestCreditBalanceMissingAccountReportsNotFound(t *testing.T) {
	ctx := context.Background()
	repo := creditFixture(t, 0)
	balance, found, err := CreditBalance(ctx, repo, "absent", 100)
	if err != nil || found || balance != 0 {
		t.Fatalf("absent account: balance=%d found=%v err=%v", balance, found, err)
	}
}

func TestCreditBalanceRejectsOverflowBeforeWriting(t *testing.T) {
	ctx := context.Background()
	repo := creditFixture(t, 0)
	account, _, _ := repo.Load(ctx, "acc")
	SetBalance(&account, 1<<62)
	if err := repo.Save(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreditBalance(ctx, repo, "acc", 1<<62); !errors.Is(err, ErrAccountCeraOverflow) {
		t.Fatalf("want overflow error, got %v", err)
	}
	reloaded, _, _ := repo.Load(ctx, "acc")
	if got := Balance(reloaded); got != 1<<62 {
		t.Fatalf("overflow must not change balance, got %d", got)
	}
}
