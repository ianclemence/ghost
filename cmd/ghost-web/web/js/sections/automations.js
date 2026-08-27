/* Ghost Section: Automations — recurring tasks Ghost runs for you. */
'use strict';

async function loadAutomations(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Automations'));
  head.appendChild(GhostUI.h('p', {}, 'Things Ghost does on a schedule.'));
  container.appendChild(head);

  const newBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showCreate(container) }, 'Create automation');
  GhostApp.setActions(newBtn);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'auto-list' });
  listEl.appendChild(GhostUI.loading('Loading automations…'));
  container.appendChild(listEl);

  let res;
  try { res = await GhostAPI.proxyGet('/v1/cron/jobs'); }
  catch (e) {
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn’t load automations', 'Ghost may still be starting.'));
    return;
  }
  renderList(listEl, res.jobs || []);
}

function renderList(listEl, jobs) {
  listEl.innerHTML = '';
  if (jobs.length === 0) {
    listEl.appendChild(GhostUI.emptyState('No automations yet', 'Schedule something Ghost should do regularly — a morning briefing, a weekly research roundup.'));
    return;
  }
  jobs.forEach(job => {
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, job.name));
    const sched = scheduleSummary(job.schedule) + (job.deliver ? '  ·  delivers to ' + (job.channel || 'mobile') : '');
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, sched));
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    const paused = !job.enabled || (job.state && job.state.lifecycle_state === 'paused');
    tr.appendChild(GhostUI.h('span', { className: 'type-foot ' + (paused ? 'text-tertiary' : ''), style: paused ? '' : 'color:var(--ok)' }, paused ? 'Paused' : 'Active'));
    tr.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: async () => {
      try { await GhostAPI.proxyPost('/v1/cron/jobs/' + job.id + '/' + (paused ? 'resume' : 'pause')); reload(); }
      catch (e) { GhostUI.toast('Couldn’t change it.', 'err'); }
    } }, paused ? 'Resume' : 'Pause'));
    row.appendChild(tr);
    listEl.appendChild(row);
  });
}

function reload() { loadAutomations(document.getElementById('view')); }

function scheduleSummary(s) {
  if (!s) return '';
  if (s.kind === 'every' && s.every_ms) {
    const mins = Math.round(s.every_ms / 60000);
    if (mins < 60) return 'Every ' + mins + ' min';
    const hrs = mins / 60;
    return 'Every ' + (hrs % 1 === 0 ? hrs : hrs.toFixed(1)) + 'h';
  }
  if (s.kind === 'cron' && s.cron) return humanCron(s.cron);
  if (s.kind === 'at' && s.at) return 'At ' + s.at;
  return s.kind || '';
}

function humanCron(expr) {
  const p = expr.split(/\s+/);
  if (p.length === 5) {
    const [m, h, dom, mon, dow] = p;
    if (dom === '*' && mon === '*' && dow === '*' && h !== '*' && m !== '*') {
      const hh = parseInt(h, 10), mm = parseInt(m, 10);
      const ap = hh >= 12 ? 'PM' : 'AM'; const hr = ((hh + 11) % 12) + 1;
      return 'Every day at ' + hr + ':' + String(mm).padStart(2, '0') + ' ' + ap;
    }
    if (dom === '*' && mon === '*' && dow === '1' && h !== '*') return 'Every Monday at ' + h + ':' + String(m || '0').padStart(2, '0');
    if (dom === '*' && mon === '*' && dow === '*/1') return 'Weekly';
  }
  return expr;
}

function showCreate(container) {
  const body = GhostUI.h('div');
  const nameField = GhostUI.h('div', { className: 'field' });
  nameField.appendChild(GhostUI.h('label', {}, 'Name'));
  const name = GhostUI.input('Morning briefing');
  nameField.appendChild(name);
  body.appendChild(nameField);

  const msgField = GhostUI.h('div', { className: 'field' });
  msgField.appendChild(GhostUI.h('label', {}, 'What should Ghost do?'));
  const msg = GhostUI.h('textarea', { className: 'ghost-input', placeholder: 'Prepare a short briefing about the things that matter to me today.' });
  msgField.appendChild(msg);
  body.appendChild(msgField);

  const schedField = GhostUI.h('div', { className: 'field' });
  schedField.appendChild(GhostUI.h('label', {}, 'Repeat'));
  const repeat = GhostUI.select([
    { value: 'daily', label: 'Every day at a time' },
    { value: 'interval', label: 'Every few minutes' },
    { value: 'cron', label: 'Custom schedule' },
  ], 'daily');
  schedField.appendChild(repeat);
  const detail = GhostUI.h('div', { style: 'margin-top:var(--s-3)' });
  schedField.appendChild(detail);
  function renderDetail() {
    detail.innerHTML = '';
    if (repeat.value === 'daily') {
      const t = GhostUI.h('input', { className: 'ghost-input', type: 'time', value: '08:00' });
      detail.appendChild(t); detail._t = t;
    } else if (repeat.value === 'interval') {
      const wrap = GhostUI.h('div', { className: 'row-flex' });
      const n = GhostUI.h('input', { className: 'ghost-input', type: 'number', min: '1', value: '30' });
      wrap.appendChild(n); wrap.appendChild(GhostUI.h('span', { className: 'text-tertiary type-foot' }, 'minutes'));
      detail.appendChild(wrap); detail._n = n;
    } else {
      const cr = GhostUI.h('input', { className: 'ghost-input type-mono', value: '0 8 * * *', placeholder: 'm h dom mon dow' });
      detail.appendChild(cr); detail._cron = cr;
    }
  }
  repeat.addEventListener('change', renderDetail);
  renderDetail();
  body.appendChild(schedField);

  body.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, 'Results are delivered to Ghost Mobile by default.'));

  GhostUI.modal('Create automation', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      if (!name.value.trim() || !msg.value.trim()) { GhostUI.toast('Name and task are required.'); return; }
      let schedule = { kind: 'cron', cron: '0 8 * * *' };
      if (repeat.value === 'daily') {
        const [h, m] = (detail._t.value || '08:00').split(':');
        schedule = { kind: 'cron', cron: parseInt(m, 10) + ' ' + parseInt(h, 10) + ' * * *' };
      } else if (repeat.value === 'interval') {
        const mins = Math.max(1, parseInt(detail._n.value, 10) || 30);
        schedule = { kind: 'every', every_ms: mins * 60000 };
      } else {
        schedule = { kind: 'cron', cron: detail._cron.value.trim() };
      }
      try {
        await GhostAPI.proxyPost('/v1/cron/jobs', { name: name.value.trim(), schedule, message: msg.value.trim(), deliver: true, channel: 'mobile' });
        e.target.closest('.ghost-modal-backdrop').remove();
        GhostUI.toast('Automation created');
        reload();
      } catch (err) { GhostUI.toast('Couldn’t create it.', 'err'); }
    } }, 'Create'),
  ]);
}

GhostApp.registerSection('automations', loadAutomations);
