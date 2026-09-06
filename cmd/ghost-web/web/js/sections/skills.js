/* Ghost Section: Skills — what Ghost knows how to do. */
'use strict';

async function loadSkills(container) {
  if (GhostApp.currentSection() !== 'skills') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Skills'));
  head.appendChild(GhostUI.h('p', {}, 'What Ghost knows how to do — enable, disable, or install skills.'));
  container.appendChild(head);

  const installBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showInstall() }, 'Install skill');
  GhostApp.setActions(installBtn);

  const loadingPanel = GhostUI.h('div', { className: 'panel' });
  loadingPanel.appendChild(GhostUI.loading('Loading skills…'));
  container.appendChild(loadingPanel);

  let res;
  try { res = await GhostAPI.proxyGet('/v1/skills'); }
  catch (e) {
    if (!document.body.contains(container)) return;
    loadingPanel.innerHTML = '';
    loadingPanel.appendChild(GhostUI.errorState('Couldn\u2019t load skills', e.message || 'Ghost may still be starting.'));
    return;
  }
  // Real readiness (enabled != configured != ready): overlay Integrations
  // status so calendar/flight/homeassistant show their true state instead
  // of the coarse optional flag.
  let integ = null;
  try { integ = await GhostAPI.get('/api/admin/integrations/status'); } catch (e) { /* offline-safe */ }
  if (!document.body.contains(container)) return;
  loadingPanel.remove();
  const skills = Array.isArray(res) ? res : (res.skills || res.items || []);
  renderSkillsList(container, skills, integ && integ.integrations);
}

// humanizeSkillDesc turns a routing-oriented SKILL.md description into
// user-facing copy: cut the "Invoke when user asks ..." trigger block,
// strip workspace paths and binary names.
function humanizeSkillDesc(desc) {
  if (!desc) return 'No description';
  let s = String(desc);
  const idx = s.search(/invoke (when|for)/i);
  if (idx > 0) s = s.slice(0, idx).trim();
  s = s.replace(/workspace\/[\w./-]+/gi, 'your workspace');
  s = s.replace(/\/var\/lib\/[\w./-]+/g, 'your workspace');
  s = s.replace(/\s*[-–—]\s*$/, '').trim();
  s = s.replace(/[.\s]+$/, '');
  return s || 'No description';
}

// skillGroups buckets skills so everyday capabilities come first and dev
// tools don't drown them. Unknown/custom skills land in More.
const SKILL_GROUPS = [
  { title: 'Everyday', sub: 'Weather, schedule, notes, and daily tasks.', skills: ['weather', 'aqi', 'daily-briefing', 'calendar', 'reminders', 'shopping', 'recipe', 'currency', 'crypto', 'calculator', 'unit-converter', 'world-clock', 'dictionary', 'translate', 'timer', 'journal', 'quick-capture', 'knowledge-base', 'find-nearby', 'travel', 'flight', 'summarize', 'scraper', 'organizer', 'healthcheck'] },
  { title: 'Smart home & media', sub: 'Devices and content that may need setup.', skills: ['homeassistant', 'camera', 'mobile', 'spotify', 'internet-reading', 'document-convert'] },
  { title: 'System & developer', sub: 'Machine tools and skill building.', skills: ['system', 'network', 'process-manager', 'tmux', 'git', 'skill-creator', 'speedtest', 'ascii-art'] },
];

// skillBadge resolves the honest state. Order:
// disabled > backend readiness (needs_setup from CheckReadiness) >
// integrations overlay (calendar/flight/homeassistant live status) >
// coarse optional flag (fallback for unknown skills).
// Amber therefore means "needs YOUR action" (connect/pair/install),
// never "this is a dev tool".
function skillBadge(s, integ) {
  const enabled = s.enabled !== 'false' && s.enabled !== false;
  if (!enabled) return { state: 'neutral', label: 'Off' };
  if (s.needs_setup === 'true' || s.needs_setup === true) {
    return { state: 'warn', label: setupLabel(s.name) };
  }
  if (s.readiness !== undefined || s.needs_setup !== undefined) {
    // Backend-provided readiness is authoritative. Anything that isn't
    // needs_setup/disabled here is ready — including ask-at-use states
    // the backend already normalized to ready. Do NOT fall through to
    // the coarse optional flag below.
    return { state: 'ready', label: '' };
  }
  if (integ) {
    if (s.name === 'calendar' && integ.calendar) {
      if (!integ.calendar.connected) return { state: 'warn', label: 'Connect' };
      return { state: 'ready', label: '' };
    }
    if (s.name === 'flight' && integ.flight) {
      if (!integ.flight.configured) return { state: 'warn', label: 'Connect' };
      return { state: 'ready', label: '' };
    }
    if (s.name === 'homeassistant' && integ.homeassistant) {
      if (!integ.homeassistant.configured) return { state: 'warn', label: 'Connect' };
      return { state: 'ready', label: '' };
    }
    if (s.name === 'camera' && integ.camera) {
      if (!integ.camera.available) return { state: 'warn', label: 'No camera' };
      return { state: 'ready', label: '' };
    }
  }
  const optional = s.optional === 'true' || s.optional === true;
  if (optional) return { state: 'warn', label: 'Needs setup' };
  return { state: 'ready', label: '' };
}

// setupLabel gives the honest action: Connect for account pairings,
// Needs setup for installable prerequisites.
function setupLabel(name) {
  if (name === 'calendar' || name === 'flight' || name === 'homeassistant' || name === 'mobile' || name === 'spotify' || name === 'email') return 'Connect';
  return 'Needs setup';
}

// Each group renders as its own panel with a panel-head h2 — the same
// pattern as System (Services, Diagnostics) and Security (Backups).
function skillGroupPanel(container, title, sub, items, integ) {
  const panel = GhostUI.h('div', { className: 'panel' });
  const head = GhostUI.h('div', { className: 'panel-head' });
  const text = GhostUI.h('div');
  text.appendChild(GhostUI.h('h2', {}, title));
  if (sub) text.appendChild(GhostUI.h('p', {}, sub));
  head.appendChild(text);
  panel.appendChild(head);
  const list = GhostUI.h('div', { className: 'ghost-list' });
  items.forEach(s => { list.appendChild(buildSkillRow(s, integ)); });
  panel.appendChild(list);
  container.appendChild(panel);
}

function renderSkillsList(container, skills, integ) {
  if (!skills || skills.length === 0) {
    const panel = GhostUI.h('div', { className: 'panel' });
    panel.appendChild(GhostUI.emptyState('No skills installed yet', 'Add your first skill from a GitHub repository, or Ghost comes with built-in skills ready to enable. Skills are the things Ghost can do for you.'));
    container.appendChild(panel);
    return;
  }
  const byName = {};
  skills.forEach(s => { byName[s.name] = s; });
  const seen = {};
  SKILL_GROUPS.forEach(g => {
    const inGroup = g.skills.map(n => byName[n]).filter(Boolean);
    if (inGroup.length === 0) return;
    inGroup.forEach(s => { seen[s.name] = true; });
    skillGroupPanel(container, g.title, g.sub, inGroup, integ);
  });
  const rest = skills.filter(s => !seen[s.name]);
  if (rest.length > 0) {
    skillGroupPanel(container, 'More', 'Custom and newly installed skills.', rest, integ);
  }
}

function buildSkillRow(s, integ) {
  const row = GhostUI.h('div', { className: 'ghost-link-row', onClick: () => openSkill(s.name) });
  const badge = skillBadge(s, integ);
  row.appendChild(GhostUI.h('span', { className: 'status-dot ' + badge.state }));
  const c = GhostUI.h('div', { className: 'ghost-row-content' });
  c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, s.name));
  c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle skill-desc' }, humanizeSkillDesc(s.description)));
  row.appendChild(c);
  const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
  if (badge.label) tr.appendChild(GhostUI.h('span', { className: 'type-foot text-tertiary' }, badge.label));
  tr.appendChild(GhostUI.h('span', { className: 'chevron' }, '\u203a'));
  row.appendChild(tr);
  return row;
}

async function openSkill(name) {
  const backdrop = GhostUI.h('div', { className: 'ghost-modal-backdrop skill-modal-backdrop' });
  const modal = GhostUI.h('div', { className: 'ghost-modal skill-modal' });
  backdrop.appendChild(modal);

  const header = GhostUI.h('div', { className: 'skill-modal-header' });
  const titleArea = GhostUI.h('div');
  const titleRow = GhostUI.h('div', { className: 'row-flex', style: 'gap:var(--s-2);align-items:center' });
  const titleDot = GhostUI.h('span', { className: 'status-dot neutral', id: 'skill-status-dot' });
  titleRow.appendChild(titleDot);
  titleRow.appendChild(GhostUI.h('h2', { className: 'skill-modal-title' }, name));
  titleArea.appendChild(titleRow);
  titleArea.appendChild(GhostUI.h('p', { className: 'skill-modal-sub', id: 'skill-modal-sub' }, 'Loading skill\u2026'));
  header.appendChild(titleArea);
  const closeBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-icon skill-modal-close', onClick: () => backdrop.remove() }, '\u2715');
  header.appendChild(closeBtn);
  modal.appendChild(header);

  const body = GhostUI.h('div', { className: 'skill-modal-body', id: 'skill-modal-body' });
  body.appendChild(GhostUI.loading('Loading skill…'));
  modal.appendChild(body);

  const actions = GhostUI.h('div', { className: 'ghost-modal-actions skill-modal-actions', id: 'skill-modal-actions' });
  modal.appendChild(actions);

  backdrop.addEventListener('click', (e) => { if (e.target === backdrop) backdrop.remove(); });
  document.body.appendChild(backdrop);

  let data;
  try { data = await GhostAPI.proxyGet('/v1/skills/read?name=' + encodeURIComponent(name)); }
  catch (e) {
    body.innerHTML = '';
    body.appendChild(GhostUI.errorState('Couldn\u2019t open this skill', 'Ghost may still be starting.'));
    const retry = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', type: 'button' }, 'Try again');
    retry.addEventListener('click', () => { backdrop.remove(); openSkill(name); });
    body.appendChild(retry);
    return;
  }

  const sub = document.getElementById('skill-modal-sub');
  const enabled = data.enabled !== false;
  const optional = !!data.optional;
  const dot = document.getElementById('skill-status-dot');
  if (dot) dot.className = 'status-dot ' + (!enabled ? 'neutral' : optional ? 'warn' : 'ready');
  const sk = (data.files || []).find(f => f.path === 'SKILL.md' || f.path === 'SKILL.md.disabled');
  const fm = GhostUI.stripFrontmatter(sk ? sk.content : '');
  const meta = fm.meta;
  const skillBody = fm.body;

  const metaParts = [];
  if (data.bundled) metaParts.push('Built-in');
  if (data.optional) metaParts.push('Needs setup');
  if (data.user_modified) metaParts.push('Modified');
  const version = GhostUI.frontmatterValue(meta, 'version');
  const author = GhostUI.frontmatterValue(meta, 'author');
  if (version) metaParts.push('v' + version);
  if (author) metaParts.push(author);
  metaParts.push(enabled ? 'Enabled' : 'Disabled');

  if (sub) {
    if (data.description) {
      sub.textContent = data.description;
      const metaEl = GhostUI.h('span', { className: 'skill-modal-meta' }, metaParts.join('  \u00b7  '));
      sub.appendChild(document.createElement('br'));
      sub.appendChild(metaEl);
    } else {
      sub.textContent = metaParts.join('  \u00b7  ');
    }
  }

  body.innerHTML = '';
  const content = GhostUI.h('div', { className: 'markdown-body' });
  content.innerHTML = GhostUI.md(skillBody || 'No documentation.');
  body.appendChild(content);

  // Show other files in this skill (excluding SKILL.md)
  const otherFiles = (data.files || []).filter(f => f.path !== 'SKILL.md' && f.path !== 'SKILL.md.disabled');
  if (otherFiles.length > 0) {
    const filesWrap = GhostUI.h('div', { style: 'margin-top:var(--s-4);border-top:1px solid var(--ink-ghost);padding-top:var(--s-3)' });
    filesWrap.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-2)' }, 'Files in this skill'));
    for (const f of otherFiles) {
      const fRow = GhostUI.h('div', { className: 'ghost-row-subtitle', style: 'padding:var(--s-1) 0;font-family:var(--font-mono);font-size:var(--t-foot)' }, f.path);
      filesWrap.appendChild(fRow);
    }
    body.appendChild(filesWrap);
  }

  actions.innerHTML = '';
  // When a skill needs credentials (calendar/flight/homeassistant), offer a
  // direct path to Integrations instead of leaving the user to guess.
  const needsSetupNames = { calendar: 1, flight: 1, homeassistant: 1, spotify: 1, email: 1 };
  if (needsSetupNames[name]) {
    const cfgBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary' }, 'Configure');
    cfgBtn.addEventListener('click', () => { backdrop.remove(); GhostApp.navigate('integrations'); });
    actions.appendChild(cfgBtn);
  }
  const toggleBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary' }, enabled ? 'Disable' : 'Enable');
  toggleBtn.addEventListener('click', async () => {
    try { await GhostAPI.proxyPost('/v1/skills/toggle', { name, enabled: !enabled }); GhostUI.toast(enabled ? 'Skill disabled' : 'Skill enabled'); backdrop.remove(); loadSkills(document.getElementById('view')); }
    catch (e) { GhostUI.toast('Couldn’t change it.', 'err'); }
  });
  actions.appendChild(toggleBtn);
  if (!(data.bundled)) {
    actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: async () => {
      if (!(await GhostUI.confirmModal('Remove this skill?', name + ' will be deleted from your Ghost. This can’t be undone.', 'Remove'))) return;
      try { await GhostAPI.post('/api/admin/skills/remove', { name }); GhostUI.toast('Removed'); backdrop.remove(); loadSkills(document.getElementById('view')); }
      catch (e) { GhostUI.toast('Couldn’t remove it.', 'err'); }
    } }, 'Remove'));
  }
}

function showInstall() {
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary', style: 'margin-bottom:var(--s-4)' }, 'Install a skill from a GitHub repository. Provide the owner, repo, and the folder path.'));
  const mk = (label, ph, key) => {
    const f = GhostUI.h('div', { className: 'field' });
    f.appendChild(GhostUI.h('label', {}, label));
    const i = GhostUI.h('input', { className: 'ghost-input', placeholder: ph });
    f.appendChild(i); body.appendChild(f); body[key] = i;
  };
  mk('Repository owner', 'ianclemence', '_owner');
  mk('Repository', 'ghost-skills', '_repo');
  mk('Path to skill folder', 'skills/research', '_path');

  GhostUI.modal('Install skill', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      const owner = body._owner.value.trim(), repo = body._repo.value.trim(), p = body._path.value.trim();
      if (!owner || !repo || !p) { GhostUI.toast('All three fields are required.'); return; }
      try {
        await GhostAPI.post('/api/admin/skills/install', { owner, repo, path: p });
        e.target.closest('.ghost-modal-backdrop').remove();
        GhostUI.toast('Skill installed');
        loadSkills(document.getElementById('view'));
      } catch (err) { GhostUI.toast('Install failed: ' + (err.message || ''), 'err'); }
    } }, 'Install'),
  ]);
}

GhostApp.registerSection('skills', loadSkills);
