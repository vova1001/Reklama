import { CITIES } from '../../content/app';
import { h } from './dom';

export class CityCombobox {
  constructor(private input: HTMLInputElement, private list: HTMLElement, onChange: () => void) {
    let active = -1;
    let matches: string[] = [];
    const close = () => {
      list.hidden = true;
      input.setAttribute('aria-expanded', 'false');
      active = -1;
      input.removeAttribute('aria-activedescendant');
    };
    const choose = (name: string) => {
      input.value = name;
      close();
      onChange();
    };
    const highlight = () => {
      list.querySelectorAll<HTMLButtonElement>('[role="option"]').forEach((b, i) => {
        b.classList.toggle('is-hi', i === active);
        b.setAttribute('aria-selected', String(i === active));
      });
      const current = list.querySelectorAll<HTMLButtonElement>('[role="option"]')[active];
      if (current) input.setAttribute('aria-activedescendant', current.id);
    };
    const render = () => {
      const q = input.value.trim().toLocaleLowerCase('ru');
      matches = CITIES.filter((name) => name.toLocaleLowerCase('ru').includes(q)).slice(0, 8);
      active = matches.length ? 0 : -1;
      list.replaceChildren(...matches.map((name, i) => {
        const option = h('button', { type: 'button', id: `${input.id}-option-${i}`, role: 'option', 'aria-selected': 'false' }, name);
        option.addEventListener('click', () => choose(name));
        return option;
      }));
      list.hidden = !matches.length || input.disabled;
      input.setAttribute('aria-expanded', String(!list.hidden));
      highlight();
    };
    input.addEventListener('focus', render);
    input.addEventListener('input', render);
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') return close();
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        if (list.hidden) render();
        if (!matches.length) return;
        active = (active + (e.key === 'ArrowDown' ? 1 : -1) + matches.length) % matches.length;
        highlight();
      } else if (e.key === 'Enter' && !list.hidden && active >= 0) {
        e.preventDefault();
        choose(matches[active]);
      }
    });
    document.addEventListener('click', (e) => {
      if (!list.contains(e.target as Node) && e.target !== input) close();
    });
  }
}
