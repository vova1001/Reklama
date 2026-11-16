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

// RewardForResult — расчёт в копейках без float. Пропорциональная оплата округляется
// вниз до 100 ₽, начинается от 100 ₽ и ограничена целевым бюджетом.
func RewardForResult(g TaskGoal, actual int64) int64 {
	if actual <= 0 || g.Target <= 0 || g.RewardKopecks <= 0 {
		return 0
	}
	if PaymentModeOrDefault(g.PaymentMode) == PaymentFixed {
		if actual < g.Target {
			return 0
		}
		return g.RewardKopecks
	}
	reward := actual * g.RewardKopecks / g.Target
	if reward > g.RewardKopecks {
		reward = g.RewardKopecks
	}
	return reward / paymentStepKopecks * paymentStepKopecks
}
