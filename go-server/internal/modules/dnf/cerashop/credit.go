package cerashop

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// AccountCeraMetadataKey 是账号共享点券在账号元数据里的键。
// 单个字段的发放路径需要直接写这一项，因此把内部常量导出，
// 避免调用方复制字符串后与这里脱节。
const AccountCeraMetadataKey = accountCeraMetadataKey

// ErrAccountCeraOverflow 表示加上本次额度会超出 int64 上限。
// 与 CreditVoucherCounts 的溢出拒绝保持一致：整笔失败，不写半个余额。
var ErrAccountCeraOverflow = errors.New("dnf account cera balance overflow")

// CreditBalance 给账号共享点券加上 delta，只落 account_cera 一项元数据，
// 不覆盖账号的其它元数据键。返回加完后的余额与账号是否存在。
//
// delta <= 0 时不做任何写入，原样返回当前余额（视作一次读取）：
// 发放方不需要额外判空，配置成 0 就等于关闭。
func CreditBalance(
	ctx context.Context,
	repo dnfrepo.AccountRepository,
	accountID string,
	delta int64,
) (balance int64, found bool, err error) {
	if repo == nil {
		return 0, false, dnfrepo.ErrAccountMetadataUnavailable
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return 0, false, dnfrepo.ErrAccountMetadataKeyRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	account, found, err := repo.Load(ctx, accountID)
	if err != nil || !found {
		return 0, found, err
	}
	current := Balance(account)
	if delta <= 0 {
		return current, true, nil
	}
	if current > math.MaxInt64-delta {
		return current, true, ErrAccountCeraOverflow
	}
	next := current + delta
	SetBalance(&account, next)
	if err := dnfrepo.SaveAccountMetadataEntry(
		ctx,
		repo,
		account,
		AccountCeraMetadataKey,
		strconv.FormatInt(next, 10),
		time.Now(),
	); err != nil {
		return current, true, err
	}
	return next, true, nil
}
