(function () {
  'use strict';
  var root = document.documentElement;

  /* theme: follows prefers-color-scheme until the visitor picks one */
  var btn = document.getElementById('themebtn');
  function current() {
    var t = root.getAttribute('data-theme');
    if (t) return t;
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }
  function label() {
    if (!btn) return;
    var next = current() === 'dark' ? 'light' : 'dark';
    btn.textContent = next === 'dark' ? 'Dark' : 'Light';
    btn.setAttribute('aria-label', 'Switch to ' + next + ' theme');
  }
  if (btn) {
    btn.addEventListener('click', function () {
      var next = current() === 'dark' ? 'light' : 'dark';
      root.setAttribute('data-theme', next);
      try { localStorage.setItem('gocpp-theme', next); } catch (e) { /* private mode */ }
      label();
    });
    label();
  }

  /* tabs */
  document.querySelectorAll('[role="tablist"]').forEach(function (list) {
    var tabs = Array.prototype.slice.call(list.querySelectorAll('[role="tab"]'));
    function select(tab, focus) {
      tabs.forEach(function (t) {
        var on = t === tab;
        t.setAttribute('aria-selected', on ? 'true' : 'false');
        t.tabIndex = on ? 0 : -1;
        document.getElementById(t.getAttribute('aria-controls')).hidden = !on;
      });
      if (focus) tab.focus();
    }
    tabs.forEach(function (t, i) {
      t.addEventListener('click', function () { select(t, false); });
      t.addEventListener('keydown', function (e) {
        if (e.key === 'ArrowRight') { select(tabs[(i + 1) % tabs.length], true); e.preventDefault(); }
        if (e.key === 'ArrowLeft') { select(tabs[(i + tabs.length - 1) % tabs.length], true); e.preventDefault(); }
      });
    });
  });

  /* copy buttons: copy the visible <pre> of the box, or data-copy */
  document.querySelectorAll('button.copy').forEach(function (b) {
    b.addEventListener('click', function () {
      var text = b.getAttribute('data-copy');
      if (!text) {
        var box = b.closest('.codebox');
        var pre = box && box.querySelector('.tabpanel:not([hidden]) pre, :scope > pre');
        text = pre ? pre.textContent : '';
      }
      function done(ok) {
        var old = b.textContent;
        b.textContent = ok ? 'Copied' : 'Press Ctrl+C';
        setTimeout(function () { b.textContent = old; }, 1400);
      }
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(function () { done(true); }, function () { done(false); });
      } else { done(false); }
    });
  });

  /* chart tooltips: every bar row carries its own values */
  var tip = document.getElementById('tip');
  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }
  function show(node, x, y) {
    tip.textContent = '';
    tip.appendChild(el('div', 'th', node.getAttribute('data-head')));
    var row = el('div', 'tr');
    var left = el('span');
    var key = el('span', 'tk');
    key.style.borderColor = 'var(--series-' + node.getAttribute('data-series') + ')';
    left.appendChild(key);
    left.appendChild(document.createTextNode(node.getAttribute('data-name')));
    row.appendChild(left);
    row.appendChild(el('b', '', node.getAttribute('data-value')));
    tip.appendChild(row);
    tip.classList.add('on');
    var w = tip.offsetWidth, h = tip.offsetHeight;
    var px = Math.min(window.innerWidth - w - 8, x + 14), py = y + 16;
    if (py + h > window.innerHeight - 8) py = y - h - 10;
    tip.style.left = Math.max(8, px) + 'px';
    tip.style.top = Math.max(8, py) + 'px';
  }
  function hide() { tip.classList.remove('on'); }
  document.querySelectorAll('svg.ch .hit').forEach(function (hit) {
    var mark = hit.nextElementSibling;
    hit.addEventListener('pointermove', function (e) { show(hit, e.clientX, e.clientY); mark.classList.add('hov'); });
    hit.addEventListener('pointerleave', function () { hide(); mark.classList.remove('hov'); });
    hit.addEventListener('focus', function () { var b = hit.getBoundingClientRect(); show(hit, b.left + b.width / 2, b.top + b.height / 2); });
    hit.addEventListener('blur', hide);
  });

  /* table view toggles */
  document.querySelectorAll('button[data-table]').forEach(function (b) {
    var t = document.getElementById(b.getAttribute('data-table'));
    b.addEventListener('click', function () {
      t.hidden = !t.hidden;
      b.setAttribute('aria-expanded', t.hidden ? 'false' : 'true');
      b.textContent = t.hidden ? 'Show as table' : 'Hide table';
    });
  });

  /* highlight the section in view in the top navigation */
  var links = {};
  document.querySelectorAll('.top nav a').forEach(function (a) { links[a.getAttribute('href').slice(1)] = a; });
  if ('IntersectionObserver' in window) {
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        if (!en.isIntersecting) return;
        for (var id in links) links[id].classList.toggle('on', id === en.target.id);
      });
    }, { rootMargin: '-30% 0px -60% 0px' });
    for (var id in links) { var s = document.getElementById(id); if (s) io.observe(s); }
  }
})();
