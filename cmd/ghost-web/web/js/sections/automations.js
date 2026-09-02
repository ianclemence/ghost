/* Ghost Section: Automations — Things Ghost will do for you. */
'use strict';

async function loadAutomations(container) {
  if (GhostApp.currentSection() !== 'automations') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Things Ghost Will Do'));
  head.appendChild(GhostUI.h('p', {}, 'Scheduled actions, reminders, and automations.'));
  container.appendChild(head);

  const newBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showCreate(container) }, 'New scheduled item');
  GhostApp.setActions(newBtn);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'auto-list' });
  listEl.appendChild(GhostUI.loading('Loading scheduled items…'));
  container.appendChild(listEl);

  let res;
  try { res = await GhostAPI.proxyGet('/v1/scheduled'); }
  catch (e) {
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn\'t load scheduled items', 'Ghost may still be starting.'));
    return;
  }
  if (!document.body.contains(container)) return;
  const items = Array.isArray(res) ? res : (res.items || []);
  renderScheduledItems(listEl, items);
}

function renderScheduledItems(listEl, items) {
  listEl.innerHTML = '';
  if (items.length === 0) {
    listEl.appendChild(GhostUI.emptyState('Nothing scheduled yet', 'Create something to have Ghost do it — a morning briefing, a daily reminder, a weekly check-in.'));
    return;
  }

  // Sort by next run time, then created time
  items.sort((a, b) => {
    if (a.next_run_at && b.next_run_at) return new Date(a.next_run_at) - new Date(b.next_run_at);
    if (a.next_run_at) return -1;
    if (b.next_run_at) return 1;
    return new Date(b.created_at) - new Date(a.created_at);
  });

  items.forEach(item => {
    const row = GhostUI.h('div', { className: 'ghost-link-row', onClick: () => showDetail(item) });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });

    // Title with type badge
    const titleRow = GhostUI.h('div', { className: 'ghost-row-title' });
    titleRow.appendChild(document.createTextNode(item.title));
    const badge = GhostUI.h('span', { className: 'type-foot', style: 'margin-left:var(--s-2);padding:2px 6px;background:var(--ink-ghost);border-radius:var(--r-sm);color:var(--ink-muted)' }, item.type);
    titleRow.appendChild(badge);
    c.appendChild(titleRow);

    // Subtitle: schedule + status
    const parts = [];
    if (item.schedule) parts.push(humanSchedule(item.schedule));
    if (item.state === 'paused') parts.push('Paused');
    else if (item.state === 'scheduled') parts.push('Active');
    else if (item.state === 'completed') parts.push('Done');
    else if (item.state === 'failed') parts.push('Failed');
    if (item.run_count > 0) parts.push('Ran ' + item.run_count + 'x');
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, parts.join('  ·  ')));

    row.appendChild(c);
    row.appendChild(GhostUI.h('span', { className: 'chevron' }, '›'));
    listEl.appendChild(row);
  });
}

function showDetail(item) {
  const container = document.getElementById('view');
  container.innerHTML = '';
  const back = GhostUI.h('div', { className: 'ghost-link-row', style: 'margin-bottom:var(--s-4);width:fit-content', onClick: () => loadAutomations(container) });
  back.appendChild(GhostUI.h('span', { className: 'chevron', style: 'transform:rotate(180deg)' }, '‹'));
  back.appendChild(GhostUI.h('span', {}, 'All scheduled items'));
  container.appendChild(back);

  const panel = GhostUI.h('div', { className: 'panel' });
  const ph = GhostUI.h('div', { className: 'panel-head' });
  const titleWrap = GhostUI.h('div');
  titleWrap.appendChild(GhostUI.h('h2', {}, item.title));
  const sub = GhostUI.h('p', {});
  const stateColor = item.state === 'completed' ? 'var(--ok)' : item.state === 'failed' ? 'var(--bad)' : item.state === 'paused' ? 'var(--ink-faint)' : 'var(--accent)';
  sub.appendChild(GhostUI.h('span', { style: 'color:' + stateColor }, capitalize(item.state)));
  sub.appendChild(document.createTextNode('  ·  ' + (item.schedule ? humanSchedule(item.schedule) : 'Manual')));
  titleWrap.appendChild(sub);
  ph.appendChild(titleWrap);

  const actions = GhostUI.h('div', { className: 'ghost-row-trailing' });
  if (item.state === 'paused') {
    actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: async () => {
      try { await GhostAPI.proxyPost('/v1/scheduled/' + item.id + '/resume'); loadAutomations(container); }
      catch (e) { GhostUI.toast('Couldn\'t resume.', 'err'); }
    } }, 'Resume'));
  } else if (item.state === 'scheduled') {
    actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: async () => {
      try { await GhostAPI.proxyPost('/v1/scheduled/' + item.id + '/pause'); loadAutomations(container); }
      catch (e) { GhostUI.toast('Couldn\'t pause.', 'err'); }
    } }, 'Pause'));
  }
  actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: async () => {
    try { await GhostAPI.proxyPost('/v1/scheduled/' + item.id + '/run'); GhostUI.toast('Triggered'); loadAutomations(container); }
    catch (e) { GhostUI.toast('Couldn\'t run.', 'err'); }
  } }, 'Run now'));
  actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-danger', onClick: async () => {
    if (!(await GhostUI.confirmModal('Delete this item?', 'This will permanently remove "' + item.title + '".', 'Delete'))) return;
    try { await GhostAPI.proxyDel('/v1/scheduled/' + item.id); GhostUI.toast('Deleted'); loadAutomations(container); }
    catch (e) { GhostUI.toast('Couldn\'t delete.', 'err'); }
  } }, 'Delete'));
  ph.appendChild(actions);
  panel.appendChild(ph);

  // Details grid
  const details = GhostUI.h('div', { style: 'margin-top:var(--s-4)' });
  if (item.run_count > 0) details.appendChild(kvRow('Times run', String(item.run_count)));
  if (item.last_run_at) details.appendChild(kvRow('Last run', GhostUI.timeAgo(Math.floor(new Date(item.last_run_at).getTime() / 1000))));
  if (item.next_run_at) details.appendChild(kvRow('Next run', GhostUI.clockTime(Math.floor(new Date(item.next_run_at).getTime() / 1000))));
  if (item.last_error) details.appendChild(kvRow('Last error', item.last_error));
  if (item.channel) details.appendChild(kvRow('Delivers to', item.channel));
  if (item.action && item.action.skills && item.action.skills.length) details.appendChild(kvRow('Skills', item.action.skills.join(', ')));
  if (details.childNodes.length > 0) panel.appendChild(details);

  // Content / prompt
  if (item.action && item.action.content) {
    const msgWrap = GhostUI.h('div', { style: 'margin-top:var(--s-4)' });
    msgWrap.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-2)' }, 'What to do'));
    const msgBody = GhostUI.h('div', { className: 'markdown-body', style: 'padding:var(--s-3);background:var(--ink-ghost);border-radius:var(--r-sm)' });
    msgBody.innerHTML = GhostUI.md(item.action.content);
    msgWrap.appendChild(msgBody);
    panel.appendChild(msgWrap);
  }

  // Shell command
  if (item.action && item.action.command) {
    const cmdWrap = GhostUI.h('div', { style: 'margin-top:var(--s-4)' });
    cmdWrap.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-2)' }, 'Shell command'));
    const code = GhostUI.h('code', { style: 'display:block;padding:var(--s-3);background:var(--ink-ghost);border-radius:var(--r-sm);font-size:var(--t-foot);word-break:break-all' }, item.action.command);
    cmdWrap.appendChild(code);
    panel.appendChild(cmdWrap);
  }

  // Execution history
  const historyEl = GhostUI.h('div', { style: 'margin-top:var(--s-4)' });
  historyEl.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-2)' }, 'Recent executions'));
  const historyList = GhostUI.h('div', { className: 'ghost-list' });
  historyList.appendChild(GhostUI.loading('Loading history…'));
  historyEl.appendChild(historyList);
  panel.appendChild(historyEl);
  container.appendChild(panel);

  // Load history
  loadHistory(historyList, item.id);
}

async function loadHistory(listEl, itemId) {
  let res;
  try { res = await GhostAPI.proxyGet('/v1/scheduled/history?item_id=' + encodeURIComponent(itemId)); }
  catch (e) {
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, 'No execution history'));
    return;
  }
  const history = Array.isArray(res) ? res : (res.history || []);
  listEl.innerHTML = '';
  if (history.length === 0) {
    listEl.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, 'No execution history'));
    return;
  }
  history.slice(0, 10).forEach(record => {
    const row = GhostUI.h('div', { className: 'ghost-row-content' });
    const statusIcon = record.status === 'ok' ? '✓' : '✗';
    const statusColor = record.status === 'ok' ? 'var(--ok)' : 'var(--bad)';
    row.appendChild(GhostUI.h('span', { style: 'color:' + statusColor + ';margin-right:var(--s-2)' }, statusIcon));
    row.appendChild(GhostUI.h('span', { className: 'type-foot text-tertiary' }, GhostUI.timeAgo(Math.floor(new Date(record.started_at).getTime() / 1000))));
    if (record.error) row.appendChild(GhostUI.h('span', { className: 'type-foot', style: 'margin-left:var(--s-2);color:var(--bad)' }, ' — ' + record.error));
    listEl.appendChild(row);
  });
}

function kvRow(label, value) {
  const r = GhostUI.h('div', { className: 'kv-row' });
  r.appendChild(GhostUI.h('div', { className: 'kv-key' }, label));
  r.appendChild(GhostUI.h('div', { className: 'kv-val' }, value));
  return r;
}

function humanSchedule(s) {
  if (!s) return '';
  if (s.kind === 'every' && s.every) {
    const mins = Math.round(s.every / 60000000000); // nanoseconds to minutes
    if (mins < 60) return 'Every ' + mins + ' min';
    const hrs = mins / 60;
    return 'Every ' + (hrs % 1 === 0 ? hrs : hrs.toFixed(1)) + 'h';
  }
  if (s.kind === 'cron' && s.expr) return humanCron(s.expr);
  if (s.kind === 'at' && s.at) return 'At ' + new Date(s.at).toLocaleString();
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

function capitalize(s) {
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : '';
}

function reload() { loadAutomations(document.getElementById('view')); }

function showCreate(container) {
  container.innerHTML = '';
  const back = GhostUI.h('div', { className: 'ghost-link-row', style: 'margin-bottom:var(--s-4);width:fit-content', onClick: () => loadAutomations(container) });
  back.appendChild(GhostUI.h('span', { className: 'chevron', style: 'transform:rotate(180deg)' }, '‹'));
  back.appendChild(GhostUI.h('span', {}, 'All scheduled items'));
  container.appendChild(back);

  const panel = GhostUI.h('div', { className: 'panel' });
  panel.appendChild(GhostUI.h('h2', {}, 'New scheduled item'));
  panel.appendChild(GhostUI.h('p', { className: 'type-callout text-secondary', style: 'margin-bottom:var(--s-4)' }, 'Tell Ghost what to do, when to do it, and where to deliver it.'));

  // Type selector
  const typeField = GhostUI.h('div', { className: 'field' });
  typeField.appendChild(GhostUI.h('label', {}, 'Type'));
  const typeSelect = GhostUI.h('select', { className: 'ghost-select' });
  const types = [
    { value: 'automation', label: 'Automation — recurring task' },
    { value: 'reminder', label: 'Reminder — one-time notification' },
    { value: 'event', label: 'Event — something happening' },
    { value: 'task', label: 'Task — durable work item' }
  ];
  types.forEach(t => typeSelect.appendChild(GhostUI.h('option', { value: t.value }, t.label)));
  typeField.appendChild(typeSelect);
  panel.appendChild(typeField);

  // Title
  const titleField = GhostUI.h('div', { className: 'field' });
  titleField.appendChild(GhostUI.h('label', {}, 'Title'));
  const titleInput = GhostUI.input('e.g. Morning briefing');
  titleField.appendChild(titleInput);
  panel.appendChild(titleField);

  // What to do
  const msgField = GhostUI.h('div', { className: 'field' });
  msgField.appendChild(GhostUI.h('label', {}, 'What should Ghost do?'));
  const msgInput = GhostUI.textarea('e.g. Summarize my calendar and today\'s news');
  msgField.appendChild(msgInput);
  panel.appendChild(msgField);

  // Schedule type
  const schedTypeField = GhostUI.h('div', { className: 'field' });
  schedTypeField.appendChild(GhostUI.h('label', {}, 'When'));
  const schedTypeSelect = GhostUI.h('select', { className: 'ghost-select' });
  schedTypeSelect.appendChild(GhostUI.h('option', { value: 'every' }, 'Repeating interval'));
  schedTypeSelect.appendChild(GhostUI.h('option', { value: 'cron' }, 'Cron schedule'));
  schedTypeSelect.appendChild(GhostUI.h('option', { value: 'at' }, 'Specific time'));
  schedTypeField.appendChild(schedTypeSelect);
  panel.appendChild(schedTypeField);

  // Schedule input
  const schedField = GhostUI.h('div', { className: 'field' });
  schedField.appendChild(GhostUI.h('label', {}, 'Schedule'));
  const schedInput = GhostUI.input('e.g. Every day at 8am, or a cron expression');
  schedField.appendChild(schedInput);
  schedField.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-top:var(--s-1)' }, 'Natural language or cron: "every 2 hours", "weekdays at 9am", "0 8 * * *"'));
  panel.appendChild(schedField);

  // Timezone
  const tzField = GhostUI.h('div', { className: 'field' });
  tzField.appendChild(GhostUI.h('label', {}, 'Timezone (optional)'));
  const tzInput = GhostUI.input('e.g. Asia/Bangkok, UTC');
  tzField.appendChild(tzInput);
  panel.appendChild(tzField);

  // Deliver to
  const deliverField = GhostUI.h('div', { className: 'field' });
  deliverField.appendChild(GhostUI.h('label', {}, 'Deliver to (optional)'));
  const channelInput = GhostUI.input('e.g. telegram, discord, mobile'));
  deliverField.appendChild(channelInput);
  panel.appendChild(deliverField);

  const errEl = GhostUI.h('div', { className: 'type-foot', style: 'color:var(--bad);min-height:18px;margin-bottom:var(--s-3)' });
  panel.appendChild(errEl);

  const submit = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', style: 'margin-top:var(--s-3)' }, 'Create');
  submit.addEventListener('click', async () => {
    const title = titleInput.value.trim();
    const content = msgInput.value.trim();
    const schedText = schedInput.value.trim();
    if (!title || !content || !schedText) { errEl.textContent = 'Title, prompt, and schedule are required.'; return; }
    submit.disabled = true;
    try {
      const schedule = parseScheduleInput(schedText, schedTypeSelect.value);
      const body = {
        type: typeSelect.value,
        title,
        description: content,
        schedule,
        action: { kind: 'agent_turn', content },
        tz: tzInput.value.trim() || 'UTC'
      };
      const ch = channelInput.value.trim();
      if (ch) { body.channel = ch; }
      await GhostAPI.proxyPost('/v1/scheduled', body);
      GhostUI.toast('Scheduled item created');
      loadAutomations(container);
    } catch (e) {
      errEl.textContent = e.message || 'Couldn\'t create item.';
      submit.disabled = false;
    }
  });
  panel.appendChild(submit);
  container.appendChild(panel);
}

function parseScheduleInput(text, kind) {
  if (kind === 'cron' || (text.match(/^[\d\*\/\-\,\s]+$/) && text.split(/\s+/).length === 5)) {
    return { kind: 'cron', expr: text };
  }
  if (kind === 'every' || text.toLowerCase().startsWith('every')) {
    const everyMin = text.match(/^every\s+(\d+)\s*m(?:in)?/i);
    if (everyMin) return { kind: 'every', every: parseInt(everyMin[1]) * 60000000000 };
    const everyHr = text.match(/^every\s+(\d+\.?\d*)\s*h/i);
    if (everyHr) return { kind: 'every', every: Math.round(parseFloat(everyHr[1]) * 3600000000000) };
  }
  return { kind: 'at', at: new Date(text).toISOString() };
}

GhostApp.registerSection('automations', loadAutomations);
