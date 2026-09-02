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

  // Keep your Ghost — the ownership promise, front and centre.
  const keepSection = GhostUI.h('section', { className: 'home-section home-keep', 'aria-labelledby': 'home-keep-title' });
  const keepHead = GhostUI.h('div', { className: 'home-section-head' });
  keepHead.appendChild(GhostUI.h('h2', { className: 'home-section-title', id: 'home-keep-title' }, 'Keep your Ghost'));
  keepSection.appendChild(keepHead);
  keepSection.appendChild(GhostUI.h('p', { className: 'type-foot text-tertiary', style: 'margin:var(--s-1) 0 var(--s-3)' }, 'Your Ghost moves with you. Back it up so you always have your memory, skills, and settings \u2014 and know how to bring it back if it ever stops.'));
  const keepActions = GhostUI.h('div', { className: 'row-flex', style: 'gap:var(--s-3);align-items:center;flex-wrap:wrap' });
  const backupBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', type: 'button' }, 'Back up my Ghost');
  backupBtn.addEventListener('click', () => GhostUI.downloadBackup(backupBtn));
  keepActions.appendChild(backupBtn);
  keepActions.appendChild(GhostUI.h('button', { className: 'home-link', type: 'button', onClick: () => recoveryModal() }, 'How recovery works  \u2192'));
  keepSection.appendChild(keepActions);
  view.appendChild(keepSection);

  container.appendChild(view);

  // Independent fetches — a single failure shouldn't blank the page.
  const [meta, doctor, health, channels, sessions, jobs, memory, selfMem, devices, ollama, activeModel] = await Promise.allSettled([
    GhostAPI.get('/api/admin/auth/meta'),
    GhostAPI.proxyGet('/v1/doctor'),
    GhostAPI.proxyGet('/v1/health'),
    GhostAPI.proxyGet('/v1/channels/status'),
    GhostAPI.proxyGet('/v1/sessions'),
    GhostAPI.proxyGet('/v1/cron/jobs'),
    GhostAPI.proxyGet('/v1/memory/files'),
    GhostAPI.proxyGet('/v1/memory/self'),
    GhostAPI.proxyGet('/v1/pairing/devices'),
    GhostAPI.get('/api/ollama/models'),
    GhostAPI.proxyGet('/v1/model'),
  ]);

  if (!document.body.contains(container)) return;

  // Personal greeting: append owner name when known.
  const ownerName = (meta.status === 'fulfilled' && meta.value && meta.value.owner_name || '').trim();
  if (ownerName) greet.textContent = greetingFor(new Date().getHours()) + ', ' + ownerName + '.';

  // Compose state.
  const overall = computeOverall(doctor, health);
  renderStatus(statusTitle, statusDot, subline, statusBody, overall, doctor, selfMem, jobs, devices, ollama, activeModel);

  // Recent activity.
  renderActivity(activityBody, sessions, jobs, memory);

  // Needs your attention.
  renderAttention(attentionBody, doctor, channels, devices, ollama);
}

function greetingFor(hour) {
  if (hour < 12) return 'Good morning';
  if (hour < 18) return 'Good afternoon';
  return 'Good evening';
}

// recoveryModal explains, in plain language, how to bring Ghost back if it ever
// stops working — using the console, not a terminal. Recovery never touches your
// memories, skills, or settings.
function recoveryModal() {
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('p', {}, 'If your Ghost ever seems stuck, try these in order. Recovery never touches your memories, skills, or settings \u2014 those stay on the device.'));
  const list = GhostUI.h('ol', { style: 'margin:var(--s-3) 0;padding-left:var(--s-5)' });
  const steps = [
    'Restart Ghost  \u2014  open the System section and choose “Restart Ghost”. This fixes most hiccups and takes a few moments.',
    'Still stuck?  Restart this device  \u2014  the hardware Ghost runs on. A minute or two of downtime is normal.',
    'If everything else fails, your backup is the safety net  \u2014  download it from the Security section, and you can bring your Ghost back from it.',
  ];
  steps.forEach(s => list.appendChild(GhostUI.h('li', { style: 'margin-bottom:var(--s-2)' }, s)));
  body.appendChild(list);
  GhostUI.modal('If Ghost seems stuck', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Close'),
  ]);
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

function renderStatus(titleEl, dotEl, sublineEl, bodyEl, overall, doctorRes, memoryRes, jobsRes, devicesRes, ollamaRes, activeModelRes) {
  dotEl.className = 'home-status-dot home-status-dot-' + overall.state;
  titleEl.textContent = overall.label;
  sublineEl.textContent = overall.detail;
  bodyEl.setAttribute('aria-busy', 'false');

  bodyEl.innerHTML = '';

  const memCount = memoryRes.status === 'fulfilled' ? extractMemoryCount(memoryRes.value) : null;
  const jobCount = jobsRes.status === 'fulfilled' ? extractJobCount(jobsRes.value) : null;
  const activeJobCount = jobsRes.status === 'fulfilled' ? extractActiveJobCount(jobsRes.value) : null;
  const devCount = devicesRes.status === 'fulfilled' ? extractDeviceCount(devicesRes.value) : null;

  const localAI = inferLocalAI(ollamaRes, activeModelRes);

  const dl = GhostUI.h('dl', { className: 'home-status-summary' });
  appendSummary(dl, 'Local AI', localAI.label);
  appendSummary(dl, 'Memory', memCount == null ? '\u2014' : GhostUI.fmtNum(memCount) + (memCount === 1 ? ' memory' : ' memories'));
  appendSummary(dl, 'Devices', devCount == null ? '\u2014' : (devCount === 0 ? 'Not connected' : devCount + ' connected'));
  appendSummary(dl, 'Automations', jobCount == null ? '\u2014' : (jobCount === 0 ? 'None' : activeJobCount + ' active of ' + jobCount));
  bodyEl.appendChild(dl);
}

function appendSummary(dl, key, value) {
  const row = GhostUI.h('div', { className: 'home-summary-row' });
  row.appendChild(GhostUI.h('dt', { className: 'home-summary-key' }, key));
  row.appendChild(GhostUI.h('dd', { className: 'home-summary-val' }, value));
  dl.appendChild(row);
}

function inferLocalAI(ollamaRes, activeModelRes) {
  // /api/ollama/models is the authoritative source for installed local models.
  // /v1/model tells us whether an active local model is set.
  if (ollamaRes.status !== 'fulfilled') return { label: 'Unavailable', state: 'bad' };
  const v = ollamaRes.value;
  if (!v || v.ok === false) return { label: 'Unavailable', state: 'bad' };
  const models = Array.isArray(v.models) ? v.models : [];
  if (models.length === 0) return { label: 'No model installed', state: 'warn' };
  // Show the active model name when it's a local (ollama) model.
  const active = (activeModelRes.status === 'fulfilled' && activeModelRes.value && activeModelRes.value.active) || '';
  const isLocal = active && /ollama/i.test(active);
  if (isLocal) {
    const f = GhostUI.modelFriendly(active);
    return { label: (f.name || shortModelName(active)) + '  \u00b7  ' + models.length + ' installed', state: 'ok' };
  }
  return { label: models.length + ' installed', state: 'ok' };
}

function shortModelName(name) {
  if (!name) return '';
  // Strip common prefixes like "ollama/" so the active model reads cleanly.
  const i = name.lastIndexOf('/');
  return i >= 0 ? name.substring(i + 1) : name;
}

function extractMemoryCount(v) {
  // Count the canonical, current memory collection Ghost relies on, not raw
  // historical rows. Duplicate extractions of the same fact count once.
  if (v && Array.isArray(v.entries)) {
    return GhostSemantic.canonicalizeEntries(v.entries).length;
  }
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
      if (isInternalTitle(rawTitle)) continue;
      const cnt = s.message_count || 0;
      items.push({
        kind: 'conversation',
        ts,
        title: rawTitle,
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
      items.push({ kind: 'automation', ts, job: j });
    }
  }

  if (memoryRes.status === 'fulfilled') {
    const arr = Array.isArray(memoryRes.value) ? memoryRes.value : (memoryRes.value.files || memoryRes.value.items || []);
    for (const m of arr) {
      const ts = m.modified || 0;
      if (!ts) continue;
      items.push({ kind: 'memory', ts, file: m });
    }
  }

  // Central semantic interpretation + grouping. Home shows the latest few,
  // collapsed, so repeated test messages never flood the feed.
  return GhostSemantic.groupItems(items.map(i => GhostSemantic.activityItem(i)));
}

// isInternalTitle filters out conversation titles that are internal identifiers
// (session ids, heartbeat, system events) rather than real user conversation.
function isInternalTitle(title) {
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
          title: c.label || prettyName(c.name),
          detail: c.message || 'This check failed.',
          cta: ctaForCheck(c.name),
          state: 'bad',
        });
      } else if (c.status === 'warning') {
        items.push({
          title: c.label || prettyName(c.name),
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
