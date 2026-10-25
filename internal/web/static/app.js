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
        if (first) window.location.href = first.href;
      } else if (e.key === 'Escape') { q.value = ''; apply(); }
    });
    document.addEventListener('keydown', function (e) {
      var t = e.target && e.target.tagName;
      if (e.key === '/' && t !== 'INPUT' && t !== 'TEXTAREA' && t !== 'SELECT') {
        e.preventDefault(); q.focus();
      }
    });
  }
  // Confirmation before destructive actions (CSP forbids inline handlers).
  document.addEventListener('submit', function (e) {
    var msg = e.target && e.target.getAttribute && e.target.getAttribute('data-confirm');
    if (msg && !window.confirm(msg)) e.preventDefault();
  });
})();
