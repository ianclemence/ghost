/* Ghost Section: Home — "Is Ghost okay? What has Ghost been doing? Does it need me?"
   Calm, personal, intentional. Not a server dashboard.

   Three questions answered top-to-bottom:
   1. Is Ghost okay?
   2. What has Ghost been doing?
   3. Does Ghost need anything from me? */

'use strict';

async function loadHome(container) {
  container.innerHTML = '';

  const view = GhostUI.h('div', { className: 'home' });

  // Header: greeting + ghost-level state sentence.
  const header = GhostUI.h('header', { className: 'home-header' });
  const greet = GhostUI.h('h1', { className: 'home-greet', id: 'home-greet' }, greetingFor(new Date().getHours()) + '.');
  header.appendChild(greet);
  const subline = GhostUI.h('p', { className: 'home-subline', id: 'home-subline', role: 'status', 'aria-live': 'polite' }, 'Reading Ghost\u2026');
  header.appendChild(subline);
  view.appendChild(header);

  // Primary status — one calm sentence + a quiet summary line.
  const statusSection = GhostUI.h('section', { className: 'home-section home-status', 'aria-labelledby': 'home-status-title' });
  const statusHead = GhostUI.h('div', { className: 'home-status-head' });
  const statusDot = GhostUI.h('span', { className: 'home-status-dot', 'aria-hidden': 'true' });
  const statusTitle = GhostUI.h('h2', { className: 'home-status-title', id: 'home-status-title' });
  statusHead.appendChild(statusDot);
  statusHead.appendChild(statusTitle);
  statusSection.appendChild(statusHead);
  const statusBody = GhostUI.h('div', { className: 'home-status-body', 'aria-busy': 'true' });
  statusSection.appendChild(statusBody);
  view.appendChild(statusSection);

  // Recent activity — quiet list with timestamps.
  const activitySection = GhostUI.h('section', { className: 'home-section home-activity', 'aria-labelledby': 'home-activity-title' });
  const activityHead = GhostUI.h('div', { className: 'home-section-head' });
  activityHead.appendChild(GhostUI.h('h2', { className: 'home-section-title', id: 'home-activity-title' }, 'Recent activity'));
  const viewAll = GhostUI.h('button', { className: 'home-link', type: 'button', onClick: () => GhostApp.navigate('activity') }, 'View all  \u2192');
  activityHead.appendChild(viewAll);
  activitySection.appendChild(activityHead);
  const activityBody = GhostUI.h('div', { className: 'home-activity-body', 'aria-busy': 'true' });
  activityBody.appendChild(activitySkeleton());
  activitySection.appendChild(activityBody);
  view.appendChild(activitySection);

  // Needs your attention — empty state or list of actionable items.
  const attentionSection = GhostUI.h('section', { className: 'home-section home-attention', 'aria-labelledby': 'home-attention-title' });
  attentionSection.appendChild(GhostUI.h('h2', { className: 'home-section-title', id: 'home-attention-title' }, 'Needs your attention'));
  const attentionBody = GhostUI.h('div', { className: 'home-attention-body', 'aria-busy': 'true' });
  attentionBody.appendChild(GhostUI.loading('Checking\u2026'));
  attentionSection.appendChild(attentionBody);
  view.appendChild(attentionSection);

  container.appendChild(view);

  // Independent fetches — a single failure shouldn't blank the page.
  const [meta, doctor, health, channels, sessions, jobs, memory, devices] = await Promise.allSettled([
    GhostAPI.get('/api/admin/auth/meta'),
    GhostAPI.proxyGet('/v1/doctor'),
    GhostAPI.proxyGet('/v1/health'),
    GhostAPI.proxyGet('/v1/channels/status'),
    GhostAPI.proxyGet('/v1/sessions'),
    GhostAPI.proxyGet('/v1/cron/jobs'),
    GhostAPI.proxyGet('/v1/memory/files'),
    GhostAPI.proxyGet('/v1/pairing/devices'),
  ]);

  if (!document.body.contains(container)) return;

  // Personal greeting: append owner name when known.
  const ownerName = (meta.status === 'fulfilled' && meta.value && meta.value.owner_name || '').trim();
  if (ownerName) greet.textContent = greetingFor(new Date().getHours()) + ', ' + ownerName + '.';

  // Compose state.
  const overall = computeOverall(doctor, health);
  renderStatus(statusTitle, statusDot, subline, statusBody, overall, doctor, memory, jobs, devices);

  // Recent activity.
  renderActivity(activityBody, sessions, jobs, memory);

  // Needs your attention.
  renderAttention(attentionBody, doctor, channels, devices);
}

function greetingFor(hour) {
  if (hour < 12) return 'Good morning';
  if (hour < 18) return 'Good afternoon';
  return 'Good evening';
}

function activitySkeleton() {
  const wrap = GhostUI.h('ul', { className: 'home-activity-list', role: 'list' });
  for (let i = 0; i < 4; i++) {
    const row = GhostUI.h('li', { className: 'home-activity-row home-activity-row-skel' });
    row.appendChild(GhostUI.h('div', { className: 'home-activity-when', 'aria-hidden': 'true' }, '\u00a0'));
    const body = GhostUI.h('div', { className: 'home-activity-body-col' });
    body.appendChild(GhostUI.h('div', { className: 'home-activity-title-skel', 'aria-hidden': 'true' }, '\u00a0'));
    body.appendChild(GhostUI.h('div', { className: 'home-activity-meta-skel', 'aria-hidden': 'true' }, '\u00a0'));
    row.appendChild(body);
    wrap.appendChild(row);
  }
  return wrap;
}

function computeOverall(doctorRes, healthRes) {
  if (healthRes.status !== 'fulfilled') {
    return { state: 'offline', label: 'Ghost is unavailable', detail: 'The Ghost service isn\u2019t responding.' };
  }
  const d = doctorRes.status === 'fulfilled' ? doctorRes.value : null;
  if (d && d.status === 'error') {
    return { state: 'bad', label: 'Ghost needs your attention', detail: 'Some Ghost capabilities aren\u2019t currently available.' };
  }
  if (d && d.status === 'warning') {
    return { state: 'warn', label: 'Ghost is up', detail: 'A few things could use a look.' };
  }
  return { state: 'ok', label: 'Ghost is healthy', detail: 'Ghost is running normally.' };
}

function renderStatus(titleEl, dotEl, sublineEl, bodyEl, overall, doctorRes, memoryRes, jobsRes, devicesRes) {
  dotEl.className = 'home-status-dot home-status-dot-' + overall.state;
  titleEl.textContent = overall.label;
  sublineEl.textContent = overall.detail;
  bodyEl.setAttribute('aria-busy', 'false');

  bodyEl.innerHTML = '';

  const checks = doctorRes.status === 'fulfilled' ? (doctorRes.value.checks || []) : [];

  const memCount = memoryRes.status === 'fulfilled' ? extractMemoryCount(memoryRes.value) : null;
  const jobCount = jobsRes.status === 'fulfilled' ? extractJobCount(jobsRes.value) : null;
  const activeJobCount = jobsRes.status === 'fulfilled' ? extractActiveJobCount(jobsRes.value) : null;
  const devCount = devicesRes.status === 'fulfilled' ? extractDeviceCount(devicesRes.value) : null;

  const localAI = inferLocalAI(checks);
  const memState = memCount == null ? 'neutral' : 'ok';
  const jobState = jobCount == null ? 'neutral' : (activeJobCount > 0 ? 'ok' : 'warn');
  const devState = devCount == null ? 'neutral' : (devCount > 0 ? 'ok' : 'info');

  const dl = GhostUI.h('dl', { className: 'home-status-summary' });
  appendSummary(dl, 'Local AI', localAI.label, localAI.state);
  appendSummary(dl, 'Memory', memCount == null ? '\u2014' : GhostUI.fmtNum(memCount) + (memCount === 1 ? ' memory' : ' memories'), memState);
  appendSummary(dl, 'Devices', devCount == null ? '\u2014' : (devCount === 0 ? 'Not connected' : devCount + ' connected'), devState);
  appendSummary(dl, 'Automations', jobCount == null ? '\u2014' : (jobCount === 0 ? 'None' : activeJobCount + ' active of ' + jobCount), jobState);
  bodyEl.appendChild(dl);
}

function appendSummary(dl, key, value, state) {
  const row = GhostUI.h('div', { className: 'home-summary-row' });
  row.appendChild(GhostUI.h('dt', { className: 'home-summary-key' }, key));
  const val = GhostUI.h('dd', { className: 'home-summary-val' });
  if (state && state !== 'neutral') {
    val.appendChild(GhostUI.h('span', { className: 'home-summary-pill home-summary-pill-' + state }, value));
  } else {
    val.textContent = value;
  }
  row.appendChild(val);
  dl.appendChild(row);
}

function inferLocalAI(checks) {
  if (!checks || checks.length === 0) return { label: '\u2014', state: 'neutral' };
  const ollama = checks.find(c => c.name === 'ollama_api' || c.name === 'ollama');
  if (!ollama) return { label: 'Ready', state: 'ok' };
  if (ollama.status === 'ok') return { label: 'Ready', state: 'ok' };
  if (ollama.status === 'warning') return { label: 'Limited', state: 'warn' };
  return { label: 'Unavailable', state: 'bad' };
}

function extractMemoryCount(v) {
  if (Array.isArray(v)) return v.length;
  if (v && typeof v === 'object') {
    const files = v.files || v.items || [];
    return Array.isArray(files) ? files.length : 0;
  }
  return 0;
}

function extractJobCount(v) {
  const arr = Array.isArray(v) ? v : (v && (v.jobs || v.items)) || [];
  return Array.isArray(arr) ? arr.length : 0;
}

function extractActiveJobCount(v) {
  const arr = Array.isArray(v) ? v : (v && (v.jobs || v.items)) || [];
  if (!Array.isArray(arr)) return 0;
  return arr.filter(j => j.enabled).length;
}

function extractDeviceCount(v) {
  const arr = Array.isArray(v) ? v : (v && (v.devices || v.items)) || [];
  return Array.isArray(arr) ? arr.length : 0;
}

function renderActivity(container, sessionsRes, jobsRes, memoryRes) {
  if (!document.body.contains(container)) return;
  container.innerHTML = '';
  container.setAttribute('aria-busy', 'false');

  const items = collectActivityItems(sessionsRes, jobsRes, memoryRes);

  if (items.length === 0) {
    container.appendChild(GhostUI.emptyState(
      'No recent activity',
      'Ghost hasn\u2019t done anything noteworthy yet. As you use Ghost, this is where it will appear.'
    ));
    return;
  }

  const list = GhostUI.h('ul', { className: 'home-activity-list', role: 'list' });
  const top = items.slice(0, 7);
  for (const it of top) {
    list.appendChild(renderActivityRow(it));
  }
  container.appendChild(list);
}

function collectActivityItems(sessionsRes, jobsRes, memoryRes) {
  const items = [];

  if (sessionsRes.status === 'fulfilled') {
    const arr = Array.isArray(sessionsRes.value) ? sessionsRes.value : (sessionsRes.value.sessions || sessionsRes.value.items || []);
    for (const s of arr) {
      const ts = s.last_activity || 0;
      if (!ts) continue;
      const rawTitle = (s.title || '').trim();
      if (isImplementationLeak(rawTitle)) continue;
      const title = humanizeTitle(rawTitle, 'Conversation');
      const cnt = s.message_count || 0;
      items.push({
        kind: 'conversation',
        ts,
        title,
        meta: cnt + ' message' + (cnt === 1 ? '' : 's'),
      });
    }
  }

  if (jobsRes.status === 'fulfilled') {
    const arr = Array.isArray(jobsRes.value) ? jobsRes.value : (jobsRes.value.jobs || jobsRes.value.items || []);
    for (const j of arr) {
      const lr = j.state && j.state.last_run_at;
      if (!lr) continue;
      const ts = Math.floor(new Date(lr).getTime() / 1000);
      const name = (j.name || 'Automation').trim();
      items.push({
        kind: 'automation',
        ts,
        title: name,
        meta: j.enabled ? 'Ran on schedule' : 'Last run',
      });
    }
  }

  if (memoryRes.status === 'fulfilled') {
    const arr = Array.isArray(memoryRes.value) ? memoryRes.value : (memoryRes.value.files || memoryRes.value.items || []);
    for (const m of arr) {
      const ts = m.modified || 0;
      if (!ts) continue;
      const name = String(m.name || '').replace(/\.md$/, '').trim();
      if (!name) continue;
      items.push({
        kind: 'memory',
        ts,
        title: memoryItemTitle(name),
        meta: 'Memory updated',
      });
    }
  }

  items.sort((a, b) => b.ts - a.ts);
  return items;
}

function isImplementationLeak(title) {
  if (!title) return true;
  const t = title.toLowerCase();
  if (t.startsWith('# heartbeat')) return true;
  if (t.startsWith('heartbeat')) return true;
  if (t.includes('cron.execute')) return true;
  if (t.includes('post /v1/chat')) return true;
  if (t.includes('ollama.generate')) return true;
  if (t.length < 3) return true;
  return false;
}

function humanizeTitle(raw, fallback) {
  if (!raw) return fallback;
  const t = raw.trim();
  if (t.length > 56) return t.substring(0, 53) + '\u2026';
  return t;
}

function memoryItemTitle(name) {
  // Memory file names are often opaque hashes; turn those into a quiet phrase
  // instead of leaking the internal identifier.
  if (!name) return 'Remembered something';
  if (/^[a-f0-9]{12,}$/i.test(name)) return 'Remembered something';
  return 'Remembered ' + name.replace(/[-_]/g, ' ');
}

function renderActivityRow(it) {
  const row = GhostUI.h('li', { className: 'home-activity-row' });
  row.appendChild(GhostUI.h('div', { className: 'home-activity-when' }, formatWhen(it.ts)));
  const body = GhostUI.h('div', { className: 'home-activity-body-col' });
  body.appendChild(GhostUI.h('div', { className: 'home-activity-title' }, it.title));
  body.appendChild(GhostUI.h('div', { className: 'home-activity-meta' }, it.meta));
  row.appendChild(body);
  return row;
}

function formatWhen(unixSec) {
  if (!unixSec) return '';
  const d = new Date(unixSec * 1000);
  const now = new Date();
  if (d.toDateString() === now.toDateString()) return formatTime(d);
  const yesterday = new Date(now); yesterday.setDate(yesterday.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday \u00b7 ' + formatTime(d);
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' \u00b7 ' + formatTime(d);
}

function formatTime(d) {
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function renderAttention(container, doctorRes, channelsRes, devicesRes) {
  if (!document.body.contains(container)) return;
  container.innerHTML = '';
  container.setAttribute('aria-busy', 'false');

  const items = collectAttentionItems(doctorRes, channelsRes, devicesRes);

  if (items.length === 0) {
    const empty = GhostUI.h('div', { className: 'home-attention-empty' });
    empty.appendChild(GhostUI.h('span', { className: 'home-attention-dot home-attention-dot-ok', 'aria-hidden': 'true' }));
    empty.appendChild(GhostUI.h('span', {}, 'Nothing right now.'));
    container.appendChild(empty);
    return;
  }

  const list = GhostUI.h('ul', { className: 'home-attention-list', role: 'list' });
  for (const it of items) {
    list.appendChild(renderAttentionItem(it));
  }
  container.appendChild(list);
}

function collectAttentionItems(doctorRes, channelsRes, devicesRes) {
  const items = [];

  // Doctor checks with errors or warnings become attention items.
  if (doctorRes.status === 'fulfilled') {
    const checks = doctorRes.value.checks || [];
    for (const c of checks) {
      if (c.status === 'error') {
        items.push({
          title: prettyName(c.name),
          detail: c.message || 'This check failed.',
          cta: ctaForCheck(c.name),
          state: 'bad',
        });
      } else if (c.status === 'warning') {
        items.push({
          title: prettyName(c.name),
          detail: c.message || 'Worth a look.',
          cta: ctaForCheck(c.name),
          state: 'warn',
        });
      }
    }
  }

  // Channels with repeated delivery failures.
  if (channelsRes.status === 'fulfilled') {
    const chs = channelsRes.value.channels || {};
    for (const name of Object.keys(chs)) {
      const raw = chs[name];
      const map = raw && typeof raw === 'object' ? raw : {};
      const failures = map.failure_count || 0;
      const lastErr = map.last_send_error || '';
      if (failures >= 3 && lastErr) {
        items.push({
          title: channelTitle(name) + ' connection lost',
          detail: failures >= 5
            ? 'Ghost hasn\u2019t been able to deliver messages through ' + channelTitle(name) + '.'
            : 'Repeated send failures on ' + channelTitle(name) + '.',
          cta: { label: 'Check channel', section: 'channels' },
          state: failures >= 5 ? 'bad' : 'warn',
        });
      }
    }
  }

  // Setup gap: no mobile device paired. Surfaced as info, not a failure.
  if (devicesRes.status === 'fulfilled') {
    const devs = Array.isArray(devicesRes.value) ? devicesRes.value : (devicesRes.value.devices || []);
    if (devs.length === 0) {
      items.push({
        title: 'Connect your phone',
        detail: 'Ghost Mobile isn\u2019t connected yet. Ghost stays on this hardware \u2014 your phone is how you take it with you.',
        cta: { label: 'Connect device', section: 'devices' },
        state: 'info',
      });
    }
  }

  return items.slice(0, 5);
}

function prettyName(s) {
  if (!s) return 'Issue';
  return s.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase());
}

function channelTitle(name) {
  const map = { telegram: 'Telegram', discord: 'Discord', slack: 'Slack', whatsapp: 'WhatsApp', email: 'Email', ghost_mobile: 'Ghost Mobile', mobile: 'Ghost Mobile' };
  return map[name] || name.charAt(0).toUpperCase() + name.slice(1);
}

function ctaForCheck(name) {
  const n = (name || '').toLowerCase();
  if (n.includes('ollama') || n.includes('model')) return { label: 'Check AI', section: 'ai' };
  if (n.includes('memory')) return { label: 'View memory', section: 'memory' };
  if (n.includes('channel')) return { label: 'Check channels', section: 'channels' };
  if (n.includes('disk') || n.includes('storage') || n.includes('service')) return { label: 'View system', section: 'system' };
  if (n.includes('auth') || n.includes('session')) return { label: 'View security', section: 'security' };
  return { label: 'View system', section: 'system' };
}

function renderAttentionItem(it) {
  const li = GhostUI.h('li', { className: 'home-attention-item home-attention-' + (it.state || 'warn') });
  const head = GhostUI.h('div', { className: 'home-attention-head' });
  head.appendChild(GhostUI.h('span', { className: 'home-attention-dot', 'aria-hidden': 'true' }));
  head.appendChild(GhostUI.h('div', { className: 'home-attention-title' }, it.title));
  li.appendChild(head);
  li.appendChild(GhostUI.h('div', { className: 'home-attention-detail' }, it.detail));
  if (it.cta && it.cta.section) {
    const link = GhostUI.h('button', {
      className: 'home-attention-cta',
      type: 'button',
      onClick: () => GhostApp.navigate(it.cta.section),
    }, it.cta.label + '  \u2192');
    li.appendChild(link);
  }
  return li;
}

GhostApp.registerSection('home', loadHome);
