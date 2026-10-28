// HomeView: tiny progressive enhancement. The dashboard works without it.
(function () {
  var q = document.getElementById('q');
  if (q) {
    var cards = Array.prototype.slice.call(document.querySelectorAll('.card'));
    var groups = Array.prototype.slice.call(document.querySelectorAll('.group'));
    var none = document.getElementById('noresults');
    var apply = function () {
      var term = q.value.trim().toLowerCase();
      document.body.classList.toggle('searching', term !== '');
      cards.forEach(function (c) {
        c.hidden = term !== '' && (c.getAttribute('data-q') || '').indexOf(term) === -1;
      });
      var shown = 0;
      groups.forEach(function (g) {
        var any = g.querySelector('.card:not([hidden])') !== null;
        g.hidden = !any;
        if (any) shown++;
      });
      if (none) none.hidden = shown !== 0;
    };
    var top = document.querySelector('.top'), sbtn = document.getElementById('searchbtn');
    var narrow = function () { return window.matchMedia('(max-width: 640px)').matches; };
    // On a narrow screen the box is only an icon until it is tapped.
    var openSearch = function () { if (narrow() && top) top.classList.add('search-open'); q.focus(); };
    var closeSearch = function () { if (top) top.classList.remove('search-open'); };
    if (sbtn) sbtn.addEventListener('click', openSearch);
    q.addEventListener('blur', function () { if (narrow() && q.value.trim() === '') closeSearch(); });
    q.addEventListener('input', apply);
    q.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') {
        var first = document.querySelector('.card:not([hidden]) .card-main');
        if (first) window.open(first.href, '_blank', 'noopener');
      } else if (e.key === 'Escape') { q.value = ''; apply(); closeSearch(); q.blur(); }
    });
    document.addEventListener('keydown', function (e) {
      var t = e.target && e.target.tagName;
      if (e.key === '/' && t !== 'INPUT' && t !== 'TEXTAREA' && t !== 'SELECT') {
        e.preventDefault(); openSearch();
      }
    });
  }
  // Service forms (new service, and "add to dashboard" per machine): picking an
  // application fills the icon, name and usual port; picking where it runs
  // fills the address. Only an empty or bare-host URL is ever rewritten.
  Array.prototype.forEach.call(document.querySelectorAll('form[data-appform]'), function (form) {
    var model = form.querySelector('.app-model');
    var origin = form.querySelector('.origin');
    var field = function (n) { return form.querySelector('[name=' + n + ']'); };
    var url = field('internal_url');
    if (!model || !url) return;
    var addr = function () {
      if (origin) {
        var oo = origin.options[origin.selectedIndex];
        return oo ? oo.getAttribute('data-addr') || '' : '';
      }
      return form.getAttribute('data-addr') || '';
    };
    var suggest = function () {
      var o = model.options[model.selectedIndex];
      var scheme = 'http', suffix = '';
      if (o && o.value && o.getAttribute('data-web') === '1') {
        scheme = o.getAttribute('data-scheme');
        var port = o.getAttribute('data-port'), path = o.getAttribute('data-path');
        var std = (scheme === 'http' && port === '80') || (scheme === 'https' && port === '443');
        suffix = (std ? '' : ':' + port) + path;
      }
      var host = addr();
      url.placeholder = scheme + '://' + (host || '192.168.1.10') + suffix;
      var cur = url.value.trim();
      var bare = /^https?:\/\/([^\/:?#]+)(:\d+)?\/?$/.exec(cur);
      var last = url.getAttribute('data-auto');
      if (!host && bare) host = bare[1]; // keep the host already typed
      if (!host) return;
      if (cur === '' || bare || cur === last) {
        url.value = scheme + '://' + host + suffix;
        url.setAttribute('data-auto', url.value);
      }
    };
    model.addEventListener('change', function () {
      var o = model.options[model.selectedIndex];
      var hint = form.querySelector('.app-hint');
      if (o.value) {
        field('icon').value = 'app:' + o.value;
        var name = field('name');
        if (!name.value.trim()) name.value = o.getAttribute('data-name');
        var label = o.getAttribute('data-portlabel');
        if (hint) hint.textContent = label ? model.getAttribute('data-port-prefix') + ' ' + label : '';
      }
      suggest();
    });
    if (origin) origin.addEventListener('change', suggest);
  });
  // Collapse/expand groups; the choice is remembered in this browser.
  (function () {
    var secs = Array.prototype.slice.call(document.querySelectorAll('.group[data-gid]'));
    if (!secs.length) return;
    var KEY = 'hv_collapsed';
    var store = {
      get: function () { try { var v = JSON.parse(localStorage.getItem(KEY) || '[]'); return Array.isArray(v) ? v : []; } catch (e) { return []; } },
      set: function (v) { try { localStorage.setItem(KEY, JSON.stringify(v)); } catch (e) { /* private mode: not remembered */ } }
    };
    var paint = function (sec, collapsed) {
      var b = sec.querySelector('.gtoggle');
      sec.classList.toggle('collapsed', collapsed);
      if (b) {
        b.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
        b.title = b.getAttribute(collapsed ? 'data-collapse-label' : 'data-expand-label');
      }
    };
    var save = function () {
      store.set(secs.filter(function (s) { return s.classList.contains('collapsed'); }).map(function (s) { return s.getAttribute('data-gid'); }));
    };
    var saved = store.get();
    secs.forEach(function (s) { paint(s, saved.indexOf(s.getAttribute('data-gid')) !== -1); });
    document.addEventListener('click', function (e) {
      var b = e.target.closest && e.target.closest('.gtoggle');
      if (!b) return;
      var sec = b.closest('.group');
      paint(sec, !sec.classList.contains('collapsed'));
      save();
    });
  })();
  // Admin: rearrange groups and cards by dragging (or Alt + Up/Down). The order is
  // saved as soon as something moves; nothing changes for other users until then.
  var reorder = document.getElementById('reorder');
  var cols = document.getElementById('cols');
  if (reorder && cols) {
    var statusEl = document.getElementById('reorder-status');
    var helpEl = document.getElementById('reorder-help');
    var on = false, drag = null, last = null;
    var all = function (sel) { return Array.prototype.slice.call(cols.querySelectorAll(sel)); };
    var snapshot = function () {
      return JSON.stringify(all('.group').map(function (sec) {
        return { group: +sec.getAttribute('data-gid'), services: all('.card').filter(function (c) { return c.closest('.group') === sec; })
          .map(function (c) { return +c.getAttribute('data-sid'); }) };
      }));
    };
    var keepOtherLast = function () { // the "other" section (id 0) always stays at the end
      var other = cols.querySelector('.group[data-gid="0"]');
      if (other && cols.lastElementChild !== other) cols.appendChild(other);
    };
    var save = function () {
      var now = snapshot();
      if (now === last) return;
      fetch(reorder.getAttribute('data-save'), {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: 'csrf=' + encodeURIComponent(reorder.getAttribute('data-csrf')) + '&layout=' + encodeURIComponent(now)
      }).then(function (r) {
        if (!r.ok) throw new Error('save failed');
        last = now; statusEl.textContent = reorder.getAttribute('data-saved');
      }).catch(function () { statusEl.textContent = reorder.getAttribute('data-failed'); });
    };
    var setMode = function (v) {
      on = v;
      document.body.classList.toggle('reorder', v);
      reorder.setAttribute('aria-pressed', v ? 'true' : 'false');
      reorder.textContent = reorder.getAttribute('data-label-done');
      reorder.hidden = !v;
      if (v) { var m = document.getElementById('menu'); if (m) m.removeAttribute('open'); reorder.focus(); }
      helpEl.hidden = !v;
      statusEl.textContent = '';
      all('.card').forEach(function (c) {
        c.draggable = v;
        Array.prototype.forEach.call(c.querySelectorAll('a,img'), function (n) { n.draggable = false; });
      });
      if (v) last = snapshot();
    };
    reorder.addEventListener('click', function () { setMode(false); });
    var menuItem = document.getElementById('reorder-menu');
    if (menuItem) { menuItem.hidden = false; menuItem.addEventListener('click', function () { setMode(true); }); }
    cols.addEventListener('click', function (e) { // no navigation while arranging
      if (on && e.target.closest && e.target.closest('.card a')) e.preventDefault();
    });
    cols.addEventListener('dragstart', function (e) {
      if (!on || !e.target.closest) return;
      var h = e.target.closest('.ghandle'), el = h ? h.closest('.group') : e.target.closest('.card');
      if (!el) return;
      drag = { kind: h ? 'group' : 'card', el: el };
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', 'homeview');
      setTimeout(function () { el.classList.add('dragging'); }, 0);
    });
    cols.addEventListener('dragover', function (e) {
      if (!drag) return;
      var sec = e.target.closest && e.target.closest('.group');
      if (!sec) return;
      e.preventDefault();
      if (drag.kind === 'group') {
        if (sec === drag.el) return;
        var r = sec.getBoundingClientRect();
        var before = e.clientY < r.top + r.height / 2;
        var ref = before ? sec : sec.nextElementSibling;
        if (ref !== drag.el) cols.insertBefore(drag.el, ref);
        keepOtherLast();
        return;
      }
      var grid = sec.querySelector('.grid');
      var cards = Array.prototype.slice.call(grid.querySelectorAll('.card')).filter(function (c) { return c !== drag.el; });
      var next = null;
      for (var i = 0; i < cards.length; i++) {
        var b = cards[i].getBoundingClientRect();
        if (e.clientY < b.top + b.height / 2 || (e.clientY < b.bottom && e.clientX < b.left + b.width / 2)) { next = cards[i]; break; }
      }
      if (drag.el.parentNode !== grid || drag.el.nextElementSibling !== next) grid.insertBefore(drag.el, next);
    });
    cols.addEventListener('drop', function (e) { if (drag) e.preventDefault(); });
    document.addEventListener('dragend', function () {
      if (!drag) return;
      drag.el.classList.remove('dragging');
      drag = null;
      save();
    });
    // Keyboard: Alt + Up/Down moves the focused card (across group edges) or group.
    cols.addEventListener('keydown', function (e) {
      if (!on || !e.altKey || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown')) return;
      var up = e.key === 'ArrowUp', t = e.target;
      var handle = t.closest && t.closest('.ghandle'), card = t.closest && t.closest('.card');
      if (handle) {
        var sec = handle.closest('.group');
        var sibs = all('.group[data-gid]:not([data-gid="0"])');
        var at = sibs.indexOf(sec), to = sibs[at + (up ? -1 : 1)];
        if (!to) return;
        cols.insertBefore(sec, up ? to : to.nextElementSibling);
        keepOtherLast(); handle.focus();
      } else if (card) {
        var grid = card.parentNode, other;
        if (up) {
          other = card.previousElementSibling;
          if (other) grid.insertBefore(card, other);
          else {
            var ps = grid.closest('.group').previousElementSibling;
            if (!ps) return;
            ps.querySelector('.grid').appendChild(card);
          }
        } else {
          other = card.nextElementSibling;
          if (other) grid.insertBefore(card, other.nextElementSibling);
          else {
            var ns = grid.closest('.group').nextElementSibling;
            if (!ns) return;
            var g2 = ns.querySelector('.grid');
            g2.insertBefore(card, g2.firstElementChild);
          }
        }
        var f = card.querySelector('.card-main'); if (f) f.focus();
      } else return;
      e.preventDefault();
      save();
    });
  }
  // Appearance: show the fade percentage while dragging the slider.
  var dim = document.querySelector('input[name=bg_dim]');
  if (dim && dim.parentNode.querySelector('output')) {
    dim.addEventListener('input', function () { dim.parentNode.querySelector('output').textContent = dim.value; });
  }
  // Header menu: a <details> that also closes on Escape and when clicking elsewhere.
  var menu = document.getElementById('menu');
  var menus = Array.prototype.slice.call(document.querySelectorAll('details.menu'));
  if (menus.length) {
    document.addEventListener('click', function (e) {
      menus.forEach(function (m) { if (m.open && !m.contains(e.target)) m.open = false; });
    });
    menus.forEach(function (m) { // opening one closes the other
      m.addEventListener('toggle', function () { if (m.open) menus.forEach(function (o) { if (o !== m) o.open = false; }); });
    });
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      menus.forEach(function (m) { if (m.open) { m.open = false; var s = m.querySelector('summary'); if (s) s.focus(); } });
    });
  }
  // Confirmation before destructive actions (CSP forbids inline handlers).
  document.addEventListener('submit', function (e) {
    var msg = e.target && e.target.getAttribute && e.target.getAttribute('data-confirm');
    if (!msg && e.submitter && e.submitter.getAttribute) msg = e.submitter.getAttribute('data-confirm-submit');
    if (msg && !window.confirm(msg)) e.preventDefault();
  });
})();

// Proxmox row: node cards on top of the dashboard, and an overlay panel with the
// machines of one node. Everything is drawn with DOM calls (no inline styles).
(function () {
  var root = document.getElementById('px');
  if (!root) return;
  var L = {};
  try { L = JSON.parse(root.getAttribute('data-l') || '{}'); } catch (e) { L = {}; }
  var every = Math.max(10, parseInt(root.getAttribute('data-refresh'), 10) || 30) * 1000;
  var url = root.getAttribute('data-url');
  var lang = document.documentElement.lang || undefined;
  var stOpen = {}; // nodes whose full storage list is expanded
  var ST_MAX = 6; // storages shown before "+N"
  var data = null, open = null, filter = 'all', collapsed = false, timer = null;
  try { collapsed = localStorage.getItem('hv_px_collapsed') === '1'; } catch (e) { /* storage may be blocked */ }

  var NS = 'http://www.w3.org/2000/svg';
  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined) n.textContent = text;
    return n;
  }
  function num(v, d) { return Number(v).toLocaleString(lang, { maximumFractionDigits: d === undefined ? 1 : d }); }
  function pct(used, total) { return total > 0 ? Math.max(0, Math.min(100, used / total * 100)) : 0; }
  function size(b) {
    var u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'], i = 0;
    while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
    return num(b, b >= 100 || i === 0 ? 0 : 1) + ' ' + u[i];
  }
  function rate(b) { return size(b) + '/s'; }
  function uptime(s) {
    var d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60);
    return d > 0 ? d + ' d ' + h + ' h' : h > 0 ? h + ' h ' + m + ' min' : m + ' min';
  }
  function lvl(p, o) { return !o.alerts ? '' : p >= o.crit ? 'bad' : p >= o.warn ? 'warn' : ''; }
  function worse(a, b) { return a === 'bad' || b === 'bad' ? 'bad' : a === 'warn' || b === 'warn' ? 'warn' : ''; }

  function gauge(label, p, sub, o) {
    var g = el('div', 'px-g ' + lvl(p, o));
    var wrap = el('div', 'px-gwrap');
    var svg = document.createElementNS(NS, 'svg');
    svg.setAttribute('viewBox', '0 0 36 36');
    svg.setAttribute('aria-hidden', 'true');
    ['tr', 'vl'].forEach(function (c) {
      var ci = document.createElementNS(NS, 'circle');
      ci.setAttribute('class', c);
      ci.setAttribute('cx', '18'); ci.setAttribute('cy', '18'); ci.setAttribute('r', '15.9');
      if (c === 'vl') ci.setAttribute('stroke-dasharray', p.toFixed(1) + ' 100');
      svg.appendChild(ci);
    });
    wrap.appendChild(svg);
    wrap.appendChild(el('b', '', num(p, 0) + '%'));
    g.appendChild(wrap);
    g.appendChild(el('span', '', label));
    if (sub) g.appendChild(el('small', '', sub));
    return g;
  }
  function bar(p, o) {
    var b = el('span', 'px-bar ' + lvl(p, o));
    var i = document.createElement('i');
    i.style.width = p.toFixed(1) + '%';
    b.appendChild(i);
    return b;
  }

  // Alert summary of a node's machines: count and worst level.
  function guestAlerts(n, o) {
    var count = 0, worst = '';
    if (!o.alerts) return { count: 0, worst: '' };
    n.guests.forEach(function (g) {
      if (!g.running) return;
      var l = worse(worse(lvl(g.cpu, o), lvl(pct(g.mem[0], g.mem[1]), o)), lvl(pct(g.disk[0], g.disk[1]), o));
      if (l) { count++; worst = worse(worst, l); }
    });
    return { count: count, worst: worst };
  }

  function nodeCard(c, n, o) {
    var mp = pct(n.mem[0], n.mem[1]);
    var level = '';
    if (n.online) {
      level = worse(lvl(n.cpu, o), lvl(mp, o));
      n.storages.forEach(function (s) { level = worse(level, lvl(pct(s.used, s.total), o)); });
    }
    var ga = guestAlerts(n, o);
    var card = el('article', 'px-card' + (n.online ? '' : ' off') + (level ? ' ' + level : ''));
    var t = el('div', 'px-title');
    var dot = el('span', 'dot ' + (n.online ? 'running' : 'stopped'));
    dot.setAttribute('role', 'img');
    dot.setAttribute('aria-label', n.online ? L.running : L.offline);
    t.appendChild(dot);
    t.appendChild(el('strong', '', n.name));
    if (n.online && n.uptime) t.appendChild(el('small', '', L.up + ' ' + uptime(n.uptime)));
    card.appendChild(t);
    var sub = [c.name];
    if (n.online && n.cpus) sub.push(n.cpus + ' ' + L.vcpu);
    if (c.err) sub.push(L.stale);
    var subEl = el('div', 'px-sub', sub.join(' · '));
    if (c.err) subEl.title = c.err;
    card.appendChild(subEl);

    if (!n.online) {
      card.appendChild(el('div', 'px-note', L.offline));
    } else {
      var gs = el('div', 'px-gauges');
      gs.appendChild(gauge(L.cpu, n.cpu, '', o));
      gs.appendChild(gauge(L.mem, mp, size(n.mem[0]) + ' / ' + size(n.mem[1]), o));
      card.appendChild(gs);
      if (n.storages.length) {
        // Two columns when the card is wide. Storages in alert come first and are
        // always shown; the rest is cut at ST_MAX behind a "+N" toggle.
        var skey = c.name + '/' + n.name;
        var rank = function (s) { var l = lvl(pct(s.used, s.total), o); return l === 'bad' ? 0 : l === 'warn' ? 1 : 2; };
        var sorted = n.storages.slice().sort(function (a, b) { return rank(a) - rank(b) || (a.name < b.name ? -1 : 1); });
        var alerting = sorted.filter(function (s) { return rank(s) < 2; }).length;
        var limit = stOpen[skey] ? sorted.length : Math.max(ST_MAX, alerting);
        var st = el('div', 'px-st');
        sorted.slice(0, limit).forEach(function (s) {
          var p = pct(s.used, s.total);
          var row = el('div', lvl(p, o));
          row.appendChild(el('span', '', s.name));
          row.appendChild(bar(p, o));
          row.appendChild(el('span', '', num(p, 0) + '%'));
          row.title = s.name + ': ' + size(s.used) + ' / ' + size(s.total);
          st.appendChild(row);
        });
        card.appendChild(st);
        if (sorted.length > Math.max(ST_MAX, alerting)) {
          var mb = el('button', 'px-stmore', stOpen[skey] ? L.stless : L.stmore.replace('{n}', String(sorted.length - limit)));
          mb.type = 'button';
          mb.setAttribute('aria-expanded', stOpen[skey] ? 'true' : 'false');
          mb.addEventListener('click', function (e) { e.stopPropagation(); stOpen[skey] = !stOpen[skey]; render(); });
          card.appendChild(mb);
        }
      }
    }
    if (o.machines && n.guests.length) {
      var key = c.name + '/' + n.name;
      var btn = el('button', 'px-more');
      btn.type = 'button';
      btn.setAttribute('aria-expanded', open === key ? 'true' : 'false');
      btn.setAttribute('data-key', key);
      btn.appendChild(el('span', '', (open === key ? L.hide : L.show) + ' (' + n.guests.length + ')'));
      if (ga.count) {
        var bd = el('span', 'px-badge ' + ga.worst, String(ga.count));
        bd.title = ga.count + ' ' + L.alertn;
        btn.appendChild(bd);
      }
      btn.addEventListener('click', function (e) {
        e.stopPropagation();
        open = open === key ? null : key;
        filter = 'all';
        render();
      });
      card.appendChild(btn);
    }
    return card;
  }

  function guestRow(g, o) {
    var r = el('div', 'px-guest' + (g.running ? '' : ' down'));
    var nm = el('div', 'px-gn');
    var dot = el('span', 'dot ' + (g.running ? 'running' : 'stopped'));
    dot.setAttribute('role', 'img');
    dot.setAttribute('aria-label', g.running ? L.running : L.stopped);
    nm.appendChild(dot);
    nm.appendChild(el('strong', '', g.name));
    nm.appendChild(el('small', '', (g.kind === 'qemu' ? L.vm : L.lxc) + ' ' + g.vmid));
    r.appendChild(nm);
    function cell(show, label, p, text) {
      if (!show) return;
      var c = el('div', 'px-cell');
      c.appendChild(el('span', 'px-cl', label)); // column title, shown on narrow screens only
      if (!g.running || p === null) { c.appendChild(el('span', '', g.running && text ? text : '—')); r.appendChild(c); return; }
      c.appendChild(bar(p, o));
      c.appendChild(el('b', '', num(p, 0) + '%'));
      c.title = text || '';
      r.appendChild(c);
    }
    cell(o.cpu, L.cpu, g.cpu, g.cpus ? g.cpus + ' ' + L.vcpu : '');
    cell(o.mem, L.mem, pct(g.mem[0], g.mem[1]), size(g.mem[0]) + ' / ' + size(g.mem[1]));
    // QEMU guests report no disk usage without the agent: show only the size.
    cell(o.disk, L.disk, g.kind === 'qemu' && g.disk[0] === 0 ? null : pct(g.disk[0], g.disk[1]), g.disk[1] ? size(g.disk[1]) : '');
    if (o.net) {
      var c = el('div', 'px-cell');
      c.appendChild(el('span', 'px-cl', L.net));
      c.appendChild(el('span', '', g.running ? '↓ ' + rate(g.in) + '  ↑ ' + rate(g.out) : '—'));
      r.appendChild(c);
    }
    return r;
  }

  function guestMatches(g, o) {
    if (filter === 'vm') return g.kind === 'qemu';
    if (filter === 'lxc') return g.kind === 'lxc';
    if (filter === 'alert') {
      return g.running && (lvl(g.cpu, o) || lvl(pct(g.mem[0], g.mem[1]), o) || lvl(pct(g.disk[0], g.disk[1]), o));
    }
    return true;
  }

  function panel(c, n, o) {
    var p = el('section', 'px-panel');
    p.setAttribute('role', 'region');
    p.setAttribute('aria-label', L.machines + ' ' + n.name);
    var h = el('div', 'px-phead');
    h.appendChild(el('strong', '', n.name + ' · ' + L.machines));
    var pills = [['all', L.all], ['vm', L.vms], ['lxc', L.lxcs]];
    if (o.alerts) pills.push(['alert', L.alerts]);
    pills.forEach(function (pl) {
      var b = el('button', 'px-pill', pl[1]);
      b.type = 'button';
      b.setAttribute('aria-pressed', filter === pl[0] ? 'true' : 'false');
      b.addEventListener('click', function (e) { e.stopPropagation(); filter = pl[0]; render(); });
      h.appendChild(b);
    });
    var x = el('button', 'px-close', '×');
    x.type = 'button';
    x.setAttribute('aria-label', L.close);
    x.addEventListener('click', function (e) { e.stopPropagation(); open = null; render(); });
    h.appendChild(x);
    p.appendChild(h);

    var cols = [o.cpu, o.mem, o.disk, o.net].filter(Boolean).length;
    var list = el('div', 'px-guests');
    list.style.setProperty('--cols', String(Math.max(1, cols)));
    var head = el('div', 'px-guest head');
    head.appendChild(el('span', '', L.machines));
    if (o.cpu) head.appendChild(el('span', '', L.cpu));
    if (o.mem) head.appendChild(el('span', '', L.mem));
    if (o.disk) head.appendChild(el('span', '', L.disk));
    if (o.net) head.appendChild(el('span', '', L.net));
    list.appendChild(head);
    var shown = n.guests.filter(function (g) { return guestMatches(g, o); });
    shown.forEach(function (g) {
      var r = guestRow(g, o);
      r.style.setProperty('--cols', String(Math.max(1, cols)));
      list.appendChild(r);
    });
    p.appendChild(list);
    if (!shown.length) p.appendChild(el('p', 'px-note', L.none));
    p.addEventListener('click', function (e) { e.stopPropagation(); });
    return p;
  }

  function place() {
    var p = root.querySelector('.px-panel'), b = root.querySelector('.px-more[aria-expanded="true"]');
    if (!p || !b) return;
    var card = b.closest('.px-card');
    p.style.top = (card.offsetTop + card.offsetHeight + 8) + 'px';
  }

  function render() {
    root.textContent = '';
    if (!data || !data.conns.length) return;
    var o = data.opts;
    var head = el('div', 'px-head');
    var tg = el('button', 'px-toggle', L.title);
    tg.type = 'button';
    tg.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
    tg.addEventListener('click', function () {
      collapsed = !collapsed;
      try { localStorage.setItem('hv_px_collapsed', collapsed ? '1' : '0'); } catch (e) { /* ignore */ }
      if (collapsed) open = null;
      render();
    });
    head.appendChild(tg);
    var newest = 0;
    data.conns.forEach(function (c) { if (c.at > newest) newest = c.at; });
    if (newest) head.appendChild(el('span', 'px-at', new Date(newest * 1000).toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit', second: '2-digit' })));
    root.appendChild(head);
    if (collapsed) return;

    var row = el('div', 'px-row');
    var openNode = null;
    data.conns.forEach(function (c) {
      if (!c.nodes.length) {
        var card = el('article', 'px-card off');
        card.appendChild(el('div', 'px-title')).appendChild(el('strong', '', c.name));
        card.appendChild(el('div', 'px-note', L.noread + (c.err ? ': ' + c.err : '')));
        row.appendChild(card);
        return;
      }
      c.nodes.forEach(function (n) {
        row.appendChild(nodeCard(c, n, o));
        if (open === c.name + '/' + n.name) openNode = { c: c, n: n };
      });
    });
    // The cards share the whole line: 1 to 4 per row, whatever the number of nodes.
    row.className = 'px-row n' + Math.max(1, Math.min(4, row.children.length));
    root.appendChild(row);
    if (openNode) {
      root.appendChild(panel(openNode.c, openNode.n, o));
      place();
    } else { open = null; }
  }

  function load() {
    if (document.hidden) return;
    fetch(url, { credentials: 'same-origin', headers: { Accept: 'application/json' } })
      .then(function (r) { if (!r.ok) throw new Error(String(r.status)); return r.json(); })
      .then(function (d) {
        data = d;
        var next = Math.max(10, d.opts.refresh || 30) * 1000;
        if (next !== every) { every = next; start(); }
        render();
      })
      .catch(function () { /* keep what is on screen; try again at the next tick */ });
  }
  function start() { if (timer) clearInterval(timer); timer = setInterval(load, every); }

  document.addEventListener('click', function () { if (open) { open = null; render(); } });
  document.addEventListener('keydown', function (e) { if (e.key === 'Escape' && open) { open = null; render(); } });
  window.addEventListener('resize', place);
  document.addEventListener('visibilitychange', function () { if (!document.hidden) load(); });
  start();
  load();
})();
