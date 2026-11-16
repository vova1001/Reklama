package domain

import "testing"

func TestRewardForResult(t *testing.T) {
	proportional := TaskGoal{Goal: GoalViews, Target: 100000, RewardKopecks: 1000000, PaymentMode: PaymentProportional}
	for _, tc := range []struct {
		actual int64
		want   int64
	}{
		{0, 0}, {999, 0}, {1000, 10000}, {1001, 10000}, {10000, 100000}, {100000, 1000000}, {150000, 1000000},
	} {
		if got := RewardForResult(proportional, tc.actual); got != tc.want {
			t.Errorf("actual=%d: got %d, want %d", tc.actual, got, tc.want)
		}
	}

	fixed := proportional
	fixed.PaymentMode = PaymentFixed
	if got := RewardForResult(fixed, 99999); got != 0 {
		t.Fatalf("фиксированная до цели: %d", got)
	}
	if got := RewardForResult(fixed, 100000); got != 1000000 {
		t.Fatalf("фиксированная при цели: %d", got)
	}
	legacy := proportional
	legacy.PaymentMode = ""
	if got := RewardForResult(legacy, 100000); got != 1000000 {
		t.Fatalf("старая цель должна быть фиксированной: %d", got)
	}
}

func TestRewardForResultBoundaries(t *testing.T) {
	g := TaskGoal{Target: 3, RewardKopecks: 10000, PaymentMode: PaymentProportional}
	for actual, want := range map[int64]int64{1: 0, 2: 0, 3: 10000, 4: 10000} {
		if got := RewardForResult(g, actual); got != want {
			t.Errorf("actual=%d: got %d, want %d", actual, got, want)
		}
	}
	g = TaskGoal{Target: 1e9, RewardKopecks: 1e8, PaymentMode: PaymentProportional}
	if got := RewardForResult(g, 1e9); got != 1e8 {
		t.Fatalf("максимальные значения: %d", got)
	}
	if got := RewardForResult(g, -1); got != 0 {
		t.Fatalf("отрицательный результат: %d", got)
	}
}
