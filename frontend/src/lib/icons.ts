// Иконки Lucide (src/icons, ISC) — встраиваются в разметку при рендере, линия 1.5 как в макете.
const files = import.meta.glob<string>('../icons/*.svg', { query: '?raw', import: 'default', eager: true });

const icons: Record<string, string> = {};
for (const [path, raw] of Object.entries(files)) {
  const name = path.split('/').pop()!.replace('.svg', '');
  icons[name] = raw
    .replace(/<!--.*?-->/gs, '')
    .replace(/<svg[^>]*>/s, (m) => m.replace(/\s(width|height|class|role)="[^"]*"/g, ''))
    .replace(/stroke-width="2"/, 'stroke-width="1.5"')
    .replace(/\n\s*/g, ' ')
    .trim();
}

export function icon(name: string, size = 24, cls = ''): string {
  const svg = icons[name];
  if (!svg) throw new Error(`Нет иконки ${name} в src/icons`);
  return svg.replace('<svg', `<svg width="${size}" height="${size}" class="ic${cls ? ' ' + cls : ''}" aria-hidden="true" focusable="false"`);
}

export const hasIcon = (name: string) => name in icons;
