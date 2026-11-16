import { goal, num } from './format';

export type PaymentMode = 'fixed' | 'proportional';
export interface RewardGoal {
  goal: string;
  target: number;
  rewardKopecks: number;
  paymentMode?: PaymentMode;
  rateKopecks?: number;
  minResult?: number;
}

export const paymentMode = (g: RewardGoal): PaymentMode => g.paymentMode ?? 'fixed';
export const moneyKopecks = (kop: number) => {
  const whole = Math.floor(kop / 100);
  const rest = kop % 100;
  return `${num(whole)}${rest ? `,${String(rest).padStart(2, '0')}` : ''} ₽`;
};
export const ratePer1000 = (g: RewardGoal) => g.rateKopecks && g.rateKopecks > 0 ? g.rateKopecks : g.target > 0 ? Number(BigInt(g.rewardKopecks) * 1000n / BigInt(g.target)) : 0;

export function rewardForResult(g: RewardGoal, actual: number): number {
  if (actual <= 0 || g.target <= 0 || g.rewardKopecks <= 0 || actual < (g.minResult ?? 0)) return 0;
  if (paymentMode(g) === 'fixed') return actual >= g.target ? g.rewardKopecks : 0;
  if (g.rateKopecks && g.rateKopecks > 0) {
    const raw = BigInt(actual) * BigInt(g.rateKopecks) / 1000n;
    return Number(raw > BigInt(g.rewardKopecks) ? BigInt(g.rewardKopecks) : raw);
  }
  const raw = BigInt(actual) * BigInt(g.rewardKopecks) / BigInt(g.target);
  const capped = raw > BigInt(g.rewardKopecks) ? BigInt(g.rewardKopecks) : raw;
  return Number(capped / 10000n * 10000n);
}

export function rewardExample(g: RewardGoal): string {
  if (!g.target || !g.rewardKopecks) return 'Заполните цель и бюджет.';
  const units: Record<string, string> = { views: 'просмотров', subscribers: 'подписчиков', clicks: 'переходов', guests: 'гостей' };
  const unit = units[g.goal] ?? units.views;
  return `${moneyKopecks(ratePer1000(g))} за 1 000 ${unit}${g.minResult ? ` · оплата от ${num(g.minResult)} ${unit}` : ''}`;
}

export function rewardTerms(g: RewardGoal): string {
  if (paymentMode(g) === 'fixed') return `${moneyKopecks(g.rewardKopecks)} при достижении цели ${goal(g.target, g.goal)}`;
  return `${rewardExample(g)} · бюджет до ${moneyKopecks(g.rewardKopecks)}`;
}
