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
        throw new Error(text || `Request failed (${res.status})`);
      }
      const ct = res.headers.get('content-type') || '';
      if (ct.includes('application/json')) return res.json();
      return res;
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
