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
  // Service form: picking a known application fills the icon and the usual port.
  var model = document.getElementById('app-model');
  if (model) {
    var field = function (n) { return document.querySelector('[name=' + n + ']'); };
    model.addEventListener('change', function () {
      var o = model.options[model.selectedIndex];
      if (!o.value) return;
      field('icon').value = 'app:' + o.value;
      var name = field('name');
      if (!name.value.trim()) name.value = o.getAttribute('data-name');
      var hint = document.getElementById('app-hint');
      var label = o.getAttribute('data-portlabel');
      hint.textContent = label ? model.getAttribute('data-port-prefix') + ' ' + label : '';
      if (o.getAttribute('data-web') !== '1') return;
      var scheme = o.getAttribute('data-scheme'), port = o.getAttribute('data-port'), path = o.getAttribute('data-path');
      var std = (scheme === 'http' && port === '80') || (scheme === 'https' && port === '443');
      var suffix = (std ? '' : ':' + port) + path;
      var url = field('internal_url');
      url.placeholder = scheme + '://192.168.1.10' + suffix;
      var m = /^https?:\/\/([^\/:?#]+)\/?$/.exec(url.value.trim());
      if (m) url.value = scheme + '://' + m[1] + suffix; // only a bare host is completed
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
