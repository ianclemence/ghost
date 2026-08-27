/* Ghost Section: Home — "Is Ghost okay? What has it been doing? Does it need me?" */
'use strict';

async function loadHome(container) {
  container.innerHTML = '';
  const view = GhostUI.h('div');

  const hour = new Date().getHours();
  const greet = hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening';

  // Greeting + status — the thesis of this page
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('div', { className: 'home-greet' }, greet + '.'));
  const presence = GhostUI.h('div', { className: 'presence', id: 'home-presence' });
  presence.appendChild(GhostUI.h('span', { className: 'text-tertiary' }, 'Checking…'));
  head.appendChild(presence);
  view.appendChild(head);

  // Activity — labeled, not just rows
  view.appendChild(GhostUI.h('div', { className: 'section-label' }, 'What Ghost has been doing'));
  const act = GhostUI.h('div', { id: 'home-activity' });
  act.appendChild(GhostUI.loading());
  view.appendChild(act);

  // Summary — directly below activity, separated by a rule
  view.appendChild(GhostUI.h('hr', { className: 'rule-tight' }));
  const summary = GhostUI.h('div', { id: 'home-summary' });
  view.appendChild(summary);

  container.appendChild(view);

  const [health, cron, skills, devices, memory] = await Promise.allSettled([
    GhostAPI.proxyGet('/v1/health'),
    GhostAPI.proxyGet('/v1/cron/jobs'),
    GhostAPI.proxyGet('/v1/skills'),
    GhostAPI.proxyGet('/v1/pairing/devices'),
    GhostAPI.proxyGet('/v1/memory/files'),
  ]);

  // Presence
  const p = document.getElementById('home-presence');
  p.innerHTML = '';
  if (health.status === 'fulfilled' && health.value.status === 'ok') {
    const uptime = (health.value.uptime_s || 0);
    const d = Math.floor(uptime / 86400), h = Math.floor((uptime % 86400) / 3600);
    const up = d > 0 ? `${d} day${d > 1 ? 's' : ''}` : `${h}h`;
    p.appendChild(GhostUI.h('span', {}, 'Ghost is running.'));
    p.appendChild(GhostUI.h('span', { className: 'text-tertiary' }, '  Up ' + up));
  } else {
    p.appendChild(GhostUI.h('span', { className: 'text-secondary' }, 'Ghost is starting up.'));
  }

  // Activity
  const a = document.getElementById('home-activity');
  a.innerHTML = '';
  const items = [];
  if (cron.status === 'fulfilled') {
    for (const job of (cron.value.jobs || [])) {
      const lr = job.state && job.state.last_run_at;
      if (lr) items.push({ ts: Math.floor(new Date(lr).getTime() / 1000), title: job.name, meta: job.enabled ? 'Ran' : 'Paused' });
    }
  }
  try {
    const sres = await GhostAPI.proxyGet('/v1/sessions');
    for (const s of (sres.sessions || []).slice(0, 6)) {
      items.push({ ts: s.last_activity, title: (s.title && s.title.trim()) ? s.title : 'Conversation', meta: GhostUI.fmtNum(s.message_count) + ' messages' });
    }
  } catch (e) { /* non-critical */ }

  items.sort((x, y) => y.ts - x.ts);
  const top = items.slice(0, 5);
  if (top.length === 0) {
    a.appendChild(GhostUI.emptyState('Quiet for now', 'Ghost hasn’t done anything yet. It will, though.'));
  } else {
    for (const it of top) {
      const row = GhostUI.h('div', { className: 'ghost-row' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, it.title));
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, GhostUI.timeAgo(it.ts)));
      row.appendChild(c);
      a.appendChild(row);
    }
  }

  // Summary
  const s = document.getElementById('home-summary');
  s.innerHTML = '';
  const skillCount = skills.status === 'fulfilled' ? (skills.value.skills || []).length : null;
  const jobCount = cron.status === 'fulfilled' ? (cron.value.jobs || []).length : null;
  const devCount = devices.status === 'fulfilled' ? (devices.value.devices || []).length : null;
  const memCount = memory.status === 'fulfilled' ? (memory.value.length || 0) : null;

  const parts = [];
  if (memCount != null && memCount > 0) parts.push(GhostUI.fmtNum(memCount) + ' memories');
  if (jobCount != null && jobCount > 0) parts.push(jobCount + ' automations');
  if (devCount != null && devCount > 0) parts.push(devCount + ' device' + (devCount > 1 ? 's' : ''));
  if (skillCount != null && skillCount > 0) parts.push(skillCount + ' skills');

  if (parts.length > 0) {
    s.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, parts.join('  ·  ')));
  }

  // Subtle setup prompt
  if (devCount === 0) {
    const prompt = GhostUI.h('div', { className: 'type-foot', style: 'margin-top:var(--s-2)' });
    const link = GhostUI.h('a', { href: '#devices', style: 'color:var(--accent);text-decoration:underline;text-underline-offset:2px' }, 'Connect your phone');
    link.addEventListener('click', e => { e.preventDefault(); GhostApp.navigate('devices'); });
    prompt.appendChild(link);
    prompt.appendChild(GhostUI.h('span', { className: 'text-tertiary' }, ' to take Ghost with you.'));
    s.appendChild(prompt);
  }
}

GhostApp.registerSection('home', loadHome);
