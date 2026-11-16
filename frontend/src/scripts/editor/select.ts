import { h } from './dom';

export interface SelectOption { value: string; title: string }

export class SelectCombobox {
  constructor(private button: HTMLButtonElement, private list: HTMLElement, options: SelectOption[], onChange: () => void) {
    let active = -1;
    const close = () => {
      list.hidden = true;
      button.setAttribute('aria-expanded', 'false');
      active = -1;
    };
    const highlight = () => {
      list.querySelectorAll<HTMLButtonElement>('[role="option"]').forEach((b, i) => {
        b.classList.toggle('is-hi', i === active);
        b.setAttribute('aria-selected', String(b.dataset.value === button.dataset.value || i === active));
      });
    };
    const open = () => {
      list.hidden = false;
      button.setAttribute('aria-expanded', 'true');
      active = Math.max(0, options.findIndex((o) => o.value === button.dataset.value));
      highlight();
    };
    const choose = (option: SelectOption) => {
      button.dataset.value = option.value;
      $('.select-value', button)!.textContent = option.title;
      const input = button.parentElement?.querySelector<HTMLInputElement>('input[type="hidden"]');
      if (input) input.value = option.value;
      close();
      onChange();
    };
    options.forEach((option, i) => {
      const item = h('button', { type: 'button', role: 'option', 'aria-selected': 'false', 'data-value': option.value }, option.title);
      item.addEventListener('click', () => choose(option));
      item.addEventListener('mousemove', () => { active = i; highlight(); });
      list.append(item);
    });
    button.addEventListener('click', () => list.hidden ? open() : close());
    button.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') return close();
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        if (list.hidden) open();
        active = (active + (e.key === 'ArrowDown' ? 1 : -1) + options.length) % options.length;
        highlight();
      } else if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        if (list.hidden) open();
        else if (active >= 0) choose(options[active]);
      }
    });
    document.addEventListener('click', (e) => {
      if (!list.contains(e.target as Node) && !button.contains(e.target as Node)) close();
    });
    highlight();
  }
}

const $ = <T extends HTMLElement = HTMLElement>(selector: string, root: ParentNode) => root.querySelector<T>(selector);
