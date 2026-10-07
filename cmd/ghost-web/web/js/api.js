/* Ghost API Client — fetch wrapper with auth handling */
'use strict';

const GhostAPI = (() => {
  let _onAuthExpired = null;

  function setOnAuthExpired(fn) { _onAuthExpired = fn; }

  async function request(path, opts = {}) {
    const { method = 'GET', body, timeout = 30000 } = opts;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeout);
    try {
      const fetchOpts = { method, signal: controller.signal, credentials: 'same-origin' };
      if (body !== undefined) {
        fetchOpts.headers = { 'Content-Type': 'application/json' };
        fetchOpts.body = JSON.stringify(body);
      }
      const res = await fetch(path, fetchOpts);
      if (res.status === 401 || res.status === 403) {
        if (_onAuthExpired) _onAuthExpired();
        throw new Error('Session expired');
      }
      if (!res.ok) {
        const text = await res.text().catch(() => '');
        // Server errors carry {error:{kind,message}}: keep the kind on the
        // thrown error so callers can explain a failure honestly instead of
        // toasting one generic string for expired, answered, and unknown.
        const err = new Error(text || `Request failed (${res.status})`);
        try {
          const body = JSON.parse(text);
          const e = body && body.error;
          if (e && typeof e === 'object') {
            if (typeof e.kind === 'string' && e.kind) err.kind = e.kind;
            if (typeof e.message === 'string' && e.message) err.message = e.message;
          }
        } catch (_) {}
        err.status = res.status;
        throw err;
      }
      const ct = res.headers.get('content-type') || '';
      if (ct.includes('application/json')) return res.json();
      const text = await res.text().catch(() => '');
      try { return JSON.parse(text); } catch (_) { return text; }
    } finally {
      clearTimeout(timer);
    }
  }

  function get(path) { return request(path); }
  function post(path, body) { return request(path, { method: 'POST', body }); }
  function put(path, body) { return request(path, { method: 'PUT', body }); }
  function patch(path, body) { return request(path, { method: 'PATCH', body }); }
  function del(path) { return request(path, { method: 'DELETE' }); }

  // Proxy helper — calls gateway API through the web server proxy
  function proxy(path, opts) { return request('/api/proxy' + path, opts); }
  function proxyGet(path) { return proxy(path); }
  function proxyPost(path, body) { return proxy(path, { method: 'POST', body }); }
  function proxyPatch(path, body) { return proxy(path, { method: 'PATCH', body }); }
  function proxyDel(path) { return proxy(path, { method: 'DELETE' }); }

  return {
    setOnAuthExpired,
    request, get, post, put, patch, del,
    proxy, proxyGet, proxyPost, proxyPatch, proxyDel
  };
})();
