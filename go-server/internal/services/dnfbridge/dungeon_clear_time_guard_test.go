package dnfbridge

import (
	"testing"
	"time"
)

func TestCurrentDungeonClearTimeIsSuspicious(t *testing.T) {
	tests := []struct {
		name      string
		elapsedMS uint32
		threshold int
		want      bool
	}{
		{name: "开关为0时不拦截", elapsedMS: 1_000, threshold: 0, want: false},
		{name: "负阈值等价于关闭", elapsedMS: 1_000, threshold: -30, want: false},
		{name: "差1毫秒到阈值算过快", elapsedMS: 29_999, threshold: 30, want: true},
		{name: "恰好等于阈值放行", elapsedMS: 30_000, threshold: 30, want: false},
		{name: "高于阈值放行", elapsedMS: 45_000, threshold: 30, want: false},
		{name: "0毫秒算过快", elapsedMS: 0, threshold: 30, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := currentDungeonClearTimeIsSuspicious(tt.elapsedMS, tt.threshold); got != tt.want {
				t.Fatalf("currentDungeonClearTimeIsSuspicious(%d, %d) = %v, want %v", tt.elapsedMS, tt.threshold, got, tt.want)
			}
		})
	}
}

func TestCurrentDungeonClearTimeBaseline(t *testing.T) {
	entry := time.Date(2026, 11, 9, 11, 0, 0, 0, time.UTC)

	t.Run("无运行时返回零值", func(t *testing.T) {
		if got := currentDungeonClearTimeBaseline(nil); !got.IsZero() {
			t.Fatalf("got %v, want zero time", got)
		}
	})

	t.Run("单人取自己进图时刻", func(t *testing.T) {
		runtime := &runtimeDungeonState{startedAt: entry}
		if got := currentDungeonClearTimeBaseline(runtime); !got.Equal(entry) {
			t.Fatalf("got %v, want %v", got, entry)
		}
	})

	t.Run("组队取队伍更早的建队时刻", func(t *testing.T) {
		runtime := &runtimeDungeonState{
			startedAt:     entry,
			partyInstance: &dungeonPartyInstance{createdAt: entry.Add(-20 * time.Second)},
		}
		if got := currentDungeonClearTimeBaseline(runtime); !got.Equal(entry.Add(-20 * time.Second)) {
			t.Fatalf("got %v, want %v", got, entry.Add(-20*time.Second))
		}
	})

	t.Run("建队时刻更晚时不采用", func(t *testing.T) {
		runtime := &runtimeDungeonState{
			startedAt:     entry,
			partyInstance: &dungeonPartyInstance{createdAt: entry.Add(5 * time.Second)},
		}
		if got := currentDungeonClearTimeBaseline(runtime); !got.Equal(entry) {
			t.Fatalf("got %v, want %v", got, entry)
		}
	})
}

func TestCurrentDungeonClearTimeGuardElapsedMilliseconds(t *testing.T) {
	entry := time.Date(2026, 11, 9, 11, 0, 0, 0, time.UTC)

	t.Run("单人按进图到通关计算", func(t *testing.T) {
		runtime := &runtimeDungeonState{
			startedAt:            entry,
			clearMapCompletionAt: entry.Add(20 * time.Second),
		}
		got, ok := currentDungeonClearTimeGuardElapsedMilliseconds(runtime)
		if !ok || got != 20_000 {
			t.Fatalf("got (%d, %v), want (20000, true)", got, ok)
		}
	})

	t.Run("组队中途加入者按建队时刻计算，避免误判", func(t *testing.T) {
		// 成员在开局 20 秒后才进来，通关发生在建队后 45 秒：
		// 按他自己的进图时刻只有 25 秒（会被误踢），按建队时刻是 45 秒（放行）。
		runtime := &runtimeDungeonState{
			startedAt:            entry,
			clearMapCompletionAt: entry.Add(25 * time.Second),
			partyInstance:        &dungeonPartyInstance{createdAt: entry.Add(-20 * time.Second)},
		}
		got, ok := currentDungeonClearTimeGuardElapsedMilliseconds(runtime)
		if !ok || got != 45_000 {
			t.Fatalf("got (%d, %v), want (45000, true)", got, ok)
		}
		if currentDungeonClearTimeIsSuspicious(got, 30) {
			t.Fatal("45 秒不应被判为过快")
		}
	})

	t.Run("通关时间缺失时不判定", func(t *testing.T) {
		runtime := &runtimeDungeonState{startedAt: entry}
		if _, ok := currentDungeonClearTimeGuardElapsedMilliseconds(runtime); ok {
			t.Fatal("缺少通关时刻时不应给出时长")
		}
	})

	t.Run("时间倒序时不判定", func(t *testing.T) {
		runtime := &runtimeDungeonState{
			startedAt:            entry,
			clearMapCompletionAt: entry.Add(-time.Second),
		}
		if _, ok := currentDungeonClearTimeGuardElapsedMilliseconds(runtime); ok {
			t.Fatal("通关早于进图时不应给出时长")
		}
	})

	t.Run("缺少通关时刻时回落到结算入口时刻", func(t *testing.T) {
		runtime := &runtimeDungeonState{
			startedAt:         entry,
			settlementEntryAt: entry.Add(40 * time.Second),
		}
		got, ok := currentDungeonClearTimeGuardElapsedMilliseconds(runtime)
		if !ok || got != 40_000 {
			t.Fatalf("got (%d, %v), want (40000, true)", got, ok)
		}
	})
}

func TestCurrentDungeonGuardPartySize(t *testing.T) {
	if got := currentDungeonGuardPartySize(nil); got != 1 {
		t.Fatalf("nil runtime party size = %d, want 1", got)
	}
	if got := currentDungeonGuardPartySize(&runtimeDungeonState{}); got != 1 {
		t.Fatalf("zero entry party count = %d, want 1", got)
	}
	if got := currentDungeonGuardPartySize(&runtimeDungeonState{entryPartyCount: 3}); got != 3 {
		t.Fatalf("entry party count 3 = %d, want 3", got)
	}
}
