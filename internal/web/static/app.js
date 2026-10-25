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
