import { goal, num } from './format';

export type PaymentMode = 'fixed' | 'proportional';
export interface RewardGoal {
  goal: string;
  target: number;
  rewardKopecks: number;
  paymentMode?: PaymentMode;
}

export const paymentMode = (g: RewardGoal): PaymentMode => g.paymentMode ?? 'fixed';
export const moneyKopecks = (kop: number) => {
  const whole = Math.floor(kop / 100);
  const rest = kop % 100;
  return `${num(whole)}${rest ? `,${String(rest).padStart(2, '0')}` : ''} ₽`;
};
export const ratePer1000 = (g: RewardGoal) => g.target > 0 ? Number(BigInt(g.rewardKopecks) * 1000n / BigInt(g.target)) : 0;

export function rewardForResult(g: RewardGoal, actual: number): number {
  if (actual <= 0 || g.target <= 0 || g.rewardKopecks <= 0) return 0;
  if (paymentMode(g) === 'fixed') return actual >= g.target ? g.rewardKopecks : 0;
  const raw = BigInt(actual) * BigInt(g.rewardKopecks) / BigInt(g.target);
  const capped = raw > BigInt(g.rewardKopecks) ? BigInt(g.rewardKopecks) : raw;
  return Number(capped / 10000n * 10000n);
}

export function rewardExample(g: RewardGoal): string {
  if (!g.target || !g.rewardKopecks) return 'Укажите цель и бюджет — покажем расчёт.';
  const example = Math.max(1, Math.floor(g.target / 10));
  return `${moneyKopecks(ratePer1000(g))} / 1 000 · ${goal(example, g.goal).replace('от ', '')} = ${moneyKopecks(rewardForResult(g, example))}. Вниз до 100 ₽.`;
}

export function rewardTerms(g: RewardGoal): string {
  if (paymentMode(g) === 'fixed') return `${moneyKopecks(g.rewardKopecks)} при достижении цели ${goal(g.target, g.goal)}`;
  return `до ${moneyKopecks(g.rewardKopecks)} за фактический результат · ${moneyKopecks(ratePer1000(g))} за 1 000 · округление вниз до 100 ₽, минимум 100 ₽`;
}
