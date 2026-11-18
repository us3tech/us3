// Progressive enhancement only: without this file every panel stays visible.
const root = document.documentElement;
root.classList.add('js');

function detectPlatform() {
  const hint = navigator.userAgentData && navigator.userAgentData.platform;
  const ua = hint || navigator.userAgent || '';
  if (/iphone|ipad|ipod/i.test(navigator.userAgent || '')) return '';
  if (/windows/i.test(ua)) return 'windows';
  // iPadOS in desktop mode reports a Macintosh UA but has a touch screen.
  if (/mac os x|macintosh|macos/i.test(ua)) return navigator.maxTouchPoints > 1 ? '' : 'macos';
  return '';
}

function setOpen(button, open) {
  const panel = document.getElementById(button.getAttribute('aria-controls'));
  button.setAttribute('aria-expanded', String(open));
  if (panel) panel.hidden = !open;
}

// Download buttons: one group per button row, at most one open panel.
const platform = detectPlatform();
for (const group of document.querySelectorAll('.dl-buttons')) {
  const buttons = [...group.querySelectorAll('button[data-platform][aria-controls]')];
  for (const b of buttons) setOpen(b, false);
  for (const b of buttons) {
    b.addEventListener('click', () => {
      const open = b.getAttribute('aria-expanded') !== 'true';
      for (const other of buttons) setOpen(other, false);
      setOpen(b, open);
    });
  }
  const pre = buttons.find((b) => b.dataset.platform === platform);
  if (pre) setOpen(pre, true);
}

// Tabs (ARIA tabs pattern, automatic activation).
function tabPlatform(tab) {
  return tab.dataset.platform || tab.getAttribute('aria-controls').replace(/^.*-/, '');
}

for (const list of document.querySelectorAll('[role=tablist]')) {
  const tabs = [...list.querySelectorAll('[role=tab][aria-controls]')];
  if (!tabs.length) continue;
  const select = (tab, focus) => {
    for (const t of tabs) {
      const on = t === tab;
      t.setAttribute('aria-selected', String(on));
      t.tabIndex = on ? 0 : -1;
      const p = document.getElementById(t.getAttribute('aria-controls'));
      if (p) p.hidden = !on;
    }
    if (focus) tab.focus();
  };
  tabs.forEach((tab, i) => {
    tab.addEventListener('click', () => select(tab, false));
    tab.addEventListener('keydown', (e) => {
      const last = tabs.length - 1;
      const to = { ArrowRight: i === last ? 0 : i + 1, ArrowLeft: i === 0 ? last : i - 1, Home: 0, End: last }[e.key];
      if (to === undefined) return;
      e.preventDefault();
      select(tabs[to], true);
    });
  });
  const fragment = location.hash.replace(/^#/, '');
  const fromHash = /^(windows|macos|synology)$/.test(fragment) && tabs.find((t) => tabPlatform(t) === fragment);
  const initial = fromHash || tabs.find((t) => tabPlatform(t) === platform) || tabs.find((t) => t.getAttribute('aria-selected') === 'true') || tabs[0];
  select(initial, false);
}

// Copy buttons: the label switches to the translated word for two seconds.
const timers = new WeakMap();
for (const b of document.querySelectorAll('button[data-copy]')) {
  b.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(b.dataset.copy);
    } catch {
      return;
    }
    if (b.dataset.label === undefined) b.dataset.label = b.textContent;
    b.textContent = b.dataset.done || b.dataset.label;
    clearTimeout(timers.get(b));
    timers.set(b, setTimeout(() => { b.textContent = b.dataset.label; }, 2000));
  });
}

// Filter (changes page): hides entries of other products.
for (const f of document.querySelectorAll('button[data-filter]')) {
  const buttons = [...document.querySelectorAll('button[data-filter]')];
  f.addEventListener('click', () => {
    const want = f.dataset.filter;
    for (const b of buttons) b.setAttribute('aria-pressed', String(b === f));
    for (const item of document.querySelectorAll('[data-product]')) {
      item.hidden = want !== 'all' && item.dataset.product !== want;
    }
  });
}
