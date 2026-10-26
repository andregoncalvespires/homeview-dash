// HomeView: tiny progressive enhancement. The dashboard works without it.
(function () {
  var q = document.getElementById('q');
  if (q) {
    var cards = Array.prototype.slice.call(document.querySelectorAll('.card'));
    var groups = Array.prototype.slice.call(document.querySelectorAll('.group'));
    var none = document.getElementById('noresults');
    var apply = function () {
      var term = q.value.trim().toLowerCase();
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
    q.addEventListener('input', apply);
    q.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') {
        var first = document.querySelector('.card:not([hidden]) .card-main');
        if (first) window.open(first.href, '_blank', 'noopener');
      } else if (e.key === 'Escape') { q.value = ''; apply(); }
    });
    document.addEventListener('keydown', function (e) {
      var t = e.target && e.target.tagName;
      if (e.key === '/' && t !== 'INPUT' && t !== 'TEXTAREA' && t !== 'SELECT') {
        e.preventDefault(); q.focus();
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
      reorder.textContent = reorder.getAttribute(v ? 'data-label-done' : 'data-label-start');
      helpEl.hidden = !v;
      statusEl.textContent = '';
      all('.card').forEach(function (c) {
        c.draggable = v;
        Array.prototype.forEach.call(c.querySelectorAll('a,img'), function (n) { n.draggable = false; });
      });
      if (v) last = snapshot();
    };
    reorder.addEventListener('click', function () { setMode(!on); });
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
  // Confirmation before destructive actions (CSP forbids inline handlers).
  document.addEventListener('submit', function (e) {
    var msg = e.target && e.target.getAttribute && e.target.getAttribute('data-confirm');
    if (msg && !window.confirm(msg)) e.preventDefault();
  });
})();
