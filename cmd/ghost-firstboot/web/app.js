/* Ghost — app.js
   Shared: API wrapper, toasts, modals, formatting. */
(function () {
    'use strict';

    const Ghost = (window.Ghost = {});

    /* ── API ── */
    Ghost.api = async function (path, method, body) {
        const opts = { method: method || 'GET', headers: {} };
        if (body !== undefined) {
            opts.headers['Content-Type'] = 'application/json';
            opts.body = JSON.stringify(body);
        }
        const res = await fetch(path, opts);
        if (res.status === 401) {
            if (Ghost.onSessionExpired) Ghost.onSessionExpired();
            throw new Error('unauthorized');
        }
        if (res.status === 204) return { ok: true };
        const ct = res.headers.get('content-type') || '';
        return ct.includes('application/json') ? res.json() : res.text();
    };

    /* ── escape ── */
    Ghost.esc = function (s) {
        return String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ({
            '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
        }[c]));
    };

    /* ── format ── */
    Ghost.fmtBytes = function (n) {
        if (!n) return '0 B';
        const units = ['B', 'KB', 'MB', 'GB', 'TB'];
        let i = 0;
        while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
        return n.toFixed(1) + ' ' + units[i];
    };

    /* ── toasts ── */
    const toastRoot = document.getElementById('toast-root');
    Ghost.toast = function (message, type) {
        if (!toastRoot) return;
        const el = document.createElement('div');
        el.className = 'toast toast--enter' + (type ? ' toast--' + type : '');
        el.setAttribute('role', type === 'err' ? 'alert' : 'status');
        el.innerHTML = '<span class="toast__dot"></span><span>' + Ghost.esc(message) + '</span>';
        toastRoot.appendChild(el);
        requestAnimationFrame(() => requestAnimationFrame(() => el.classList.remove('toast--enter')));
        const leave = () => {
            el.classList.add('toast--leave');
            el.addEventListener('transitionend', () => el.remove(), { once: true });
            setTimeout(() => el.remove(), 400);
        };
        setTimeout(leave, type === 'err' ? 5000 : 2800);
    };
    Ghost.toast.ok = (m) => Ghost.toast(m, 'ok');
    Ghost.toast.err = (m) => Ghost.toast(m, 'err');

    /* ── modal confirm ── */
    Ghost.confirm = function ({ title, body, okLabel = 'Confirm', danger = false }) {
        return new Promise((resolve) => {
            const root = document.getElementById('modal-root');
            const backdrop = document.createElement('div');
            backdrop.className = 'modal-backdrop';
            backdrop.innerHTML =
                '<div class="modal" role="alertdialog" aria-modal="true" aria-labelledby="ghost-modal-title">' +
                '<h3 class="modal__title" id="ghost-modal-title">' + Ghost.esc(title) + '</h3>' +
                (body ? '<p class="modal__body">' + Ghost.esc(body) + '</p>' : '') +
                '<div class="modal__actions">' +
                '<button class="btn btn--secondary modal__cancel" type="button">Cancel</button>' +
                '<button class="btn ' + (danger ? 'btn--danger' : 'btn--primary') + ' modal__ok" type="button">' + Ghost.esc(okLabel) + '</button>' +
                '</div></div>';
            const close = (val) => {
                document.removeEventListener('keydown', onKey);
                backdrop.remove();
                resolve(val);
            };
            const onKey = (e) => { if (e.key === 'Escape') close(false); };
            backdrop.addEventListener('click', (e) => { if (e.target === backdrop) close(false); });
            backdrop.querySelector('.modal__cancel').addEventListener('click', () => close(false));
            backdrop.querySelector('.modal__ok').addEventListener('click', () => close(true));
            document.addEventListener('keydown', onKey);
            root.appendChild(backdrop);
            backdrop.querySelector('.modal__ok').focus();
        });
    };

    /* ── helpers for sections ── */
    Ghost.$ = (id) => document.getElementById(id);
    Ghost.show = (el) => { if (el) el.classList.remove('hidden'); };
    Ghost.hide = (el) => { if (el) el.classList.add('hidden'); };
    Ghost.msg = function (id, text, ok) {
        const el = Ghost.$(id);
        if (!el) return;
        el.textContent = text || '';
        el.classList.remove('hidden');
        el.classList.toggle('ok', !!ok);
        if (!text) el.classList.add('hidden');
    };
})();