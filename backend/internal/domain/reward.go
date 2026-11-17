package domain

const (
	PaymentFixed        = "fixed"
	PaymentProportional = "proportional"
	paymentStepKopecks  = int64(10000)
)

func ValidPaymentMode(mode string) bool {
	return mode == PaymentFixed || mode == PaymentProportional
}

func PaymentModeOrDefault(mode string) string {
	if mode == "" {
		return PaymentFixed
	}
	return mode
}

// RewardForResult — расчёт в копейках без float. Новая ставка задаётся за 1000 результатов;
// для старых целей она выводится из бюджета и цели. Выплата ограничена бюджетом.
func RewardForResult(g TaskGoal, actual int64) int64 {
	if actual <= 0 || g.Target <= 0 || g.RewardKopecks <= 0 || actual < g.MinResult {
		return 0
	}
	if g.QuotaResult > 0 && g.QuotaRewardKopecks > 0 {
		if actual >= g.Target {
			return g.RewardKopecks
		}
		reward := actual / g.QuotaResult * g.QuotaRewardKopecks
		if reward > g.RewardKopecks {
			reward = g.RewardKopecks
		}
		return reward
	}
	if PaymentModeOrDefault(g.PaymentMode) == PaymentFixed {
		if actual < g.Target {
			return 0
		}
		return g.RewardKopecks
	}
	if g.RateKopecks > 0 {
		reward := actual * g.RateKopecks / 1000
		if reward > g.RewardKopecks {
			reward = g.RewardKopecks
		}
		return reward
	}
	reward := actual * g.RewardKopecks / g.Target
	if reward > g.RewardKopecks {
		reward = g.RewardKopecks
	}
	return reward / paymentStepKopecks * paymentStepKopecks
}
