/* Ghost Section: Automations \u2014 recurring tasks Ghost runs for you. */
'use strict';

async function loadAutomations(container) {
  if (GhostApp.currentSection() !== 'automations') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Automations'));
  head.appendChild(GhostUI.h('p', {}, 'Things Ghost does on a schedule.'));
  container.appendChild(head);

  const newBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showCreate(container) }, 'Create automation');
  GhostApp.setActions(newBtn);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'auto-list' });
  listEl.appendChild(GhostUI.loading('Loading automations\u2026'));
  container.appendChild(listEl);

  let res;
  try { res = await GhostAPI.proxyGet('/v1/cron/jobs'); }
  catch (e) {
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn\u2019t load automations', 'Ghost may still be starting.'));
    return;
  }
  if (!document.body.contains(container)) return;
  const jobs = Array.isArray(res) ? res : (res.jobs || res.items || []);
  renderList(listEl, jobs);
}

function renderList(listEl, jobs) {
  listEl.innerHTML = '';
  if (jobs.length === 0) {
    listEl.appendChild(GhostUI.emptyState('No automations yet', 'Create one to have Ghost do something on a schedule \u2014 a morning briefing, a weekly research roundup, a daily check-in.'));
    return;
  }
  jobs.forEach(job => {
    const row = GhostUI.h('div', { className: 'ghost-link-row', onClick: () => showDetail(job) });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, job.name));
    const paused = !job.enabled || job.lifecycle_state === 'paused';
    const parts = [scheduleSummary(job.schedule)];
    if (job.payload && job.payload.deliver) parts.push('delivers to ' + (job.payload.channel || 'mobile'));
    parts.push(paused ? 'Paused' : 'Active');
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, parts.join('  \u00b7  ')));
    row.appendChild(c);
    row.appendChild(GhostUI.h('span', { className: 'chevron' }, '\u203a'));
    listEl.appendChild(row);
  });
}

function showDetail(job) {
  const container = document.getElementById('view');
  container.innerHTML = '';
  const back = GhostUI.h('div', { className: 'ghost-link-row', style: 'margin-bottom:var(--s-4);width:fit-content', onClick: () => loadAutomations(container) });
  back.appendChild(GhostUI.h('span', { className: 'chevron', style: 'transform:rotate(180deg)' }, '\u2039'));
  back.appendChild(GhostUI.h('span', {}, 'All automations'));
  container.appendChild(back);

  const paused = !job.enabled || job.lifecycle_state === 'paused';
  const panel = GhostUI.h('div', { className: 'panel' });
  const ph = GhostUI.h('div', { className: 'panel-head' });
  const titleWrap = GhostUI.h('div');
  titleWrap.appendChild(GhostUI.h('h2', {}, job.name));
  const sub = GhostUI.h('p', {});
  sub.appendChild(GhostUI.h('span', { style: paused ? 'color:var(--ink-faint)' : 'color:var(--ok)' }, paused ? 'Paused' : 'Active'));
  sub.appendChild(document.createTextNode('  \u00b7  ' + scheduleSummary(job.schedule)));
  titleWrap.appendChild(sub);
  ph.appendChild(titleWrap);
  const actions = GhostUI.h('div', { className: 'ghost-row-trailing' });
  actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: async () => {
    try { await GhostAPI.proxyPost('/v1/cron/jobs/' + job.id + '/' + (paused ? 'resume' : 'pause')); loadAutomations(container); }
    catch (e) { GhostUI.toast('Couldn\u2019t change it.', 'err'); }
  } }, paused ? 'Resume' : 'Pause'));
  actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-danger', onClick: async () => {
    if (!(await GhostUI.confirmModal('Delete this automation?', 'This will permanently remove \u201c' + job.name + '\u201d.', 'Delete'))) return;
    try { await GhostAPI.proxyDel('/v1/cron/jobs/' + job.id); GhostUI.toast('Automation deleted'); loadAutomations(container); }
    catch (e) { GhostUI.toast('Couldn\u2019t delete.', 'err'); }
  } }, 'Delete'));
  ph.appendChild(actions);
  panel.appendChild(ph);

  // Details grid
  const details = GhostUI.h('div', { style: 'margin-top:var(--s-4)' });
  if (job.run_count > 0) details.appendChild(kvRow('Times run', String(job.run_count)));
  if (job.last_run_at) details.appendChild(kvRow('Last run', GhostUI.timeAgo(Math.floor(new Date(job.last_run_at).getTime() / 1000))));
  if (job.next_run_at) details.appendChild(kvRow('Next run', GhostUI.clockTime(Math.floor(new Date(job.next_run_at).getTime() / 1000))));
  if (job.state && job.state.lastError) details.appendChild(kvRow('Last error', job.state.lastError));
  if (job.payload && job.payload.deliver) details.appendChild(kvRow('Delivers to', job.payload.channel || 'mobile'));
  if (job.payload && job.payload.skills && job.payload.skills.length) details.appendChild(kvRow('Skills', job.payload.skills.join(', ')));
  if (details.childNodes.length > 0) panel.appendChild(details);

  // Prompt / message
  if (job.payload && job.payload.message) {
    const msgWrap = GhostUI.h('div', { style: 'margin-top:var(--s-4)' });
    msgWrap.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-2)' }, 'Prompt'));
    const msgBody = GhostUI.h('div', { className: 'markdown-body', style: 'padding:var(--s-3);background:var(--ink-ghost);border-radius:var(--r-sm)' });
    msgBody.innerHTML = GhostUI.md(job.payload.message);
    msgWrap.appendChild(msgBody);
    panel.appendChild(msgWrap);
  }

  // Shell command
  if (job.payload && job.payload.command) {
    const cmdWrap = GhostUI.h('div', { style: 'margin-top:var(--s-4)' });
    cmdWrap.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-2)' }, 'Shell command'));
    const code = GhostUI.h('code', { style: 'display:block;padding:var(--s-3);background:var(--ink-ghost);border-radius:var(--r-sm);font-size:var(--t-foot);word-break:break-all' }, job.payload.command);
    cmdWrap.appendChild(code);
    panel.appendChild(cmdWrap);
  }

  container.appendChild(panel);
}

function kvRow(label, value) {
  const r = GhostUI.h('div', { className: 'kv-row' });
  r.appendChild(GhostUI.h('div', { className: 'kv-label' }, label));
  r.appendChild(GhostUI.h('div', { className: 'kv-value' }, value));
  return r;
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
    if (dom === '*' && mon === '*' && dow === '5' && h !== '*') return 'Every Friday at ' + h + ':' + String(m || '0').padStart(2, '0');
  }
  return expr;
}

function showCreate(container) {
  container.innerHTML = '';
  const back = GhostUI.h('div', { className: 'ghost-link-row', style: 'margin-bottom:var(--s-4);width:fit-content', onClick: () => loadAutomations(container) });
  back.appendChild(GhostUI.h('span', { className: 'chevron', style: 'transform:rotate(180deg)' }, '\u2039'));
  back.appendChild(GhostUI.h('span', {}, 'All automations'));
  container.appendChild(back);

  const panel = GhostUI.h('div', { className: 'panel' });
  panel.appendChild(GhostUI.h('h2', {}, 'New automation'));
  panel.appendChild(GhostUI.h('p', { className: 'type-callout text-secondary', style: 'margin-bottom:var(--s-4)' }, 'Tell Ghost what to do, when to do it, and where to deliver it.'));

  const nameField = GhostUI.h('div', { className: 'field' });
  nameField.appendChild(GhostUI.h('label', {}, 'Name'));
  const nameInput = GhostUI.input('e.g. Morning briefing');
  nameField.appendChild(nameInput);
  panel.appendChild(nameField);

  const msgField = GhostUI.h('div', { className: 'field' });
  msgField.appendChild(GhostUI.h('label', {}, 'What should Ghost do?'));
  const msgInput = GhostUI.textarea('e.g. Summarize my calendar and today\u2019s news');
  msgField.appendChild(msgInput);
  panel.appendChild(msgField);

  const schedField = GhostUI.h('div', { className: 'field' });
  schedField.appendChild(GhostUI.h('label', {}, 'Schedule'));
  const schedInput = GhostUI.input('e.g. Every day at 8am, or a cron expression');
  schedField.appendChild(schedInput);
  schedField.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-top:var(--s-1)' }, 'Natural language or cron: "every 2 hours", "weekdays at 9am", "0 8 * * *"'));
  panel.appendChild(schedField);

  const deliverField = GhostUI.h('div', { className: 'field' });
  deliverField.appendChild(GhostUI.h('label', {}, 'Deliver to (optional)'));
  const channelInput = GhostUI.input('e.g. mobile, telegram, discord');
  deliverField.appendChild(channelInput);
  panel.appendChild(deliverField);

  const errEl = GhostUI.h('div', { className: 'type-foot', style: 'color:var(--bad);min-height:18px;margin-bottom:var(--s-3)' });
  panel.appendChild(errEl);

  const submit = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', style: 'margin-top:var(--s-3)' }, 'Create');
  submit.addEventListener('click', async () => {
    const name = nameInput.value.trim();
    const message = msgInput.value.trim();
    const schedText = schedInput.value.trim();
    if (!name || !message || !schedText) { errEl.textContent = 'Name, prompt, and schedule are required.'; return; }
    submit.disabled = true;
    try {
      const schedule = parseScheduleInput(schedText);
      const body = { name, message, schedule };
      const ch = channelInput.value.trim();
      if (ch) { body.deliver = true; body.channel = ch; }
      await GhostAPI.proxyPost('/v1/cron/jobs', body);
      GhostUI.toast('Automation created');
      loadAutomations(container);
    } catch (e) {
      errEl.textContent = e.message || 'Couldn\u2019t create automation.';
      submit.disabled = false;
    }
  });
  panel.appendChild(submit);
  container.appendChild(panel);
}

function parseScheduleInput(text) {
  // If it looks like a cron expression (5 space-separated fields, all numbers/wildcards)
  const cronMatch = text.match(/^[\d\*\/\-\,\s]+$/);
  if (cronMatch && text.split(/\s+/).length === 5) {
    return { kind: 'cron', expr: text };
  }
  // "every Xm" / "every Xh"
  const everyMin = text.match(/^every\s+(\d+)\s*m(?:in)?/i);
  if (everyMin) return { kind: 'every', everyMs: parseInt(everyMin[1]) * 60000 };
  const everyHr = text.match(/^every\s+(\d+\.?\d*)\s*h/i);
  if (everyHr) return { kind: 'every', everyMs: Math.round(parseFloat(everyHr[1]) * 3600000) };
  // Try as at
  return { kind: 'at', at: text };
}

GhostApp.registerSection('automations', loadAutomations);
