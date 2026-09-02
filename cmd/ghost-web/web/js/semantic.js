/* Ghost Semantic Presentation — a single layer that turns internal Ghost state
   into human language any person understands.

   Internal values (journal paths, date codes, raw predicates, scheduled ids,
   raw extraction sentences) are USED by the rest of Ghost but must never appear
   in normal UI. Everything in this module is pure: it converts a structured
   internal value into a user-facing { title, meta } pair, and it never calls a
   model or mutates anything.

   Loaded BEFORE the section files so sections can share it. */
'use strict';

const GhostSemantic = (() => {

  // ── Memory / journal note classification ─────────────────────────────────
  // Memory files on disk follow internal conventions that must not reach the
  // UI: YYYYMM/YYYYMMDD.md (daily notes), -briefing.md (briefings), MEMORY.md.

  const DATE_FILE_RE = /(?:^|[\/\\])(\d{6})\/(\d{8})(?:-([a-z0-9-]+))?\.md$/i;
  const BARE_DATE_RE = /^\d{4}-\d{2}-\d{2}$/;
  const MEMORY_FILE_RE = /^MEMORY\.md$/i;
  const BRIEFING_RE = /briefing/i;

  // parseDateCode turns an internal "YYYYMMDD" (or "YYYY-MM-DD") into a Date.
  function parseDateCode(code) {
    if (/^\d{8}$/.test(code)) {
      return new Date(+code.slice(0, 4), +code.slice(4, 6) - 1, +code.slice(6, 8));
    }
    if (/^\d{4}-\d{2}-\d{2}$/.test(code)) {
      const p = code.split('-');
      return new Date(+p[0], +p[1] - 1, +p[2]);
    }
    return null;
  }

  // humanDate renders a Date as "Sep 2, 2026" without a time component.
  function humanDate(d) {
    return d.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' });
  }

  // relativeDay renders a Date relative to *now*: "Today", "Yesterday", or
  // "Sep 2, 2026". Dates are computed in the viewer's local timezone.
  function relativeDay(d) {
    const today = new Date();
    const y = new Date(); y.setDate(y.getDate() - 1);
    if (d.toDateString() === today.toDateString()) return 'Today';
    if (d.toDateString() === y.toDateString()) return 'Yesterday';
    return humanDate(d);
  }

  // decodeNote returns { isDaily, isBriefing, date, slug } for a memory file
  // given its name and parsed title. It is the bridge between the internal
  // file naming and the semantic presentation of a note.
  function decodeNote(file) {
    const name = (file && file.name) || '';
    const title = (file && file.title) || '';
    const bare = (file && file.title || '').trim();

    const m = name.match(DATE_FILE_RE);
    if (m) {
      const date = parseDateCode(m[2]);
      const slug = m[3] || '';
      const isBriefing = BRIEFING_RE.test(slug) || BRIEFING_RE.test(bare);
      return { isDaily: !isBriefing, isBriefing, date: date || null, slug };
    }
    if (MEMORY_FILE_RE.test(name)) {
      return { isDaily: false, isBriefing: false, date: null, slug: '', isMemory: true };
    }
    if (BARE_DATE_RE.test(bare)) {
      return { isDaily: true, isBriefing: false, date: parseDateCode(bare), slug: '' };
    }
    return { isDaily: false, isBriefing: false, date: null, slug: '' };
  }

  // isJournalNote reports whether a memory file is an internal continuity
  // record (a plain daily journal note) rather than ordinary user memory or a
  // user-facing briefing. Journal records are presented in their own clearly
  // labelled context, never as a fact Ghost learned about the user.
  function isJournalNote(file) {
    const dec = decodeNote(file);
    return dec.isDaily && !dec.slug;
  }

  // noteTitle returns a display title for a memory file. It prefers the human
  // title Ghost already stored, and only decodes internal date codes when the
  // stored title is itself an internal token (e.g. a bare date or a path).
  function noteTitle(file) {
    const raw = (file && file.title || '').trim();
    const name = (file && file.name || '').trim();

    if (!raw || BARE_DATE_RE.test(raw) || raw === 'Memory') {
      const dec = decodeNote(file);
      if (dec.isMemory) return 'Memory';
      if (dec.isBriefing) {
        return briefingTitle(name);
      }
      if (dec.isDaily) {
        const when = dec.date ? relativeDay(dec.date) : null;
        return when === 'Today' ? "Today's notes" : when === 'Yesterday' ? "Yesterday's notes" : (when || 'Notes');
      }
      if (raw && !BARE_DATE_RE.test(raw) && raw !== 'Memory') return raw;
      return prettyName(name);
    }

    // Human title stored by Ghost. For briefings like "Weekly Briefing —
    // 2026-09-02 (Wednesday)", keep the short noun form.
    if (BRIEFING_RE.test(raw)) return briefingTitle(name);
    return raw;
  }

  // briefingTitle turns a briefing file name or title into its short noun form
  // ("Weekly briefing"). Date codes and "— date (weekday)" suffixes are
  // stripped so the internal identifier never reaches the UI.
  function briefingTitle(text) {
    let s = String(text || '').replace(/\.md$/i, '');
    s = s.replace(/[\/\\_]+/g, ' ');
    // Title form like "Weekly Briefing — 2026-09-02 (Wednesday)": drop the date.
    s = s.split(/\s*[—]\s*/)[0];
    // Remove date tokens now that separators are gone.
    s = s.replace(/\b\d{6}\s+\d{8}\b/g, ' ')
         .replace(/\b\d{8}\b/g, ' ')
         .replace(/\b\d{4}-\d{2}-\d{2}\b/g, ' ')
         .replace(/[-]+/g, ' ');
    s = s.replace(/\s+/g, ' ').trim();
    if (!s || /^briefing$/i.test(s)) return 'Briefing';
    return s.split(/\s+/).map((w, i) => i === 0 ? capitalize(w) : w.toLowerCase()).join(' ');
  }

  // prettyName turns an opaque file stem into spaced words ("2026-09-02" style
  // tokens become nothing; hashes become nothing).
  function prettyName(name) {
    let stem = String(name).replace(/\.md$/i, '').split(/[\/\\]/).pop() || '';
    stem = stem.replace(/[-_]/g, ' ');
    if (/^[0-9]{6}[ ]?[0-9]{6,8}$/.test(stem.replace(/ /g, ''))) return '';
    if (/^[a-f0-9]{12,}$/i.test(stem.replace(/ /g, ''))) return '';
    if (!stem.trim()) return '';
    return capitalize(stem.trim());
  }

  function capitalize(s) {
    return s.charAt(0).toUpperCase() + s.slice(1);
  }

  // memoryActivity gives a user-facing { title, meta } for a memory file event
  // in an activity feed. Never exposes the path or date code.
  function memoryActivity(file) {
    const dec = decodeNote(file);
    const modified = file && file.modified;
    if (dec.isMemory) {
      return { title: 'Memory updated', meta: 'Memory updated', groupKey: 'memory:MEMORY' };
    }
    if (dec.isBriefing) {
      const t = briefingTitle(file.name);
      return { title: t + ' updated', meta: 'Memory updated', groupKey: 'note:briefing' };
    }
    if (dec.isDaily) {
      const when = dec.date ? relativeDay(dec.date) : null;
      const t = when === 'Today' ? "Today's notes" : when === 'Yesterday' ? "Yesterday's notes" : (when || 'Notes');
      return { title: t + ' updated', meta: 'Notes saved', groupKey: 'note:journal' };
    }
    const t = noteTitle(file) || 'Memory updated';
    return { title: t, meta: 'Memory updated', groupKey: 'memory:note' };
  }

  // ── Conversations ────────────────────────────────────────────────────────
  // conversationTitle normalizes a stored session title. It never exposes a
  // session id and never shows an internal path or date code.
  function conversationTitle(raw) {
    const t = (raw || '').trim();
    if (!t) return 'Conversation';
    if (isInternalToken(t)) return 'Conversation';
    return collapse(t);
  }

  function isInternalToken(t) {
    const low = t.toLowerCase();
    if (low.startsWith('#') || /^[a-f0-9]{12,}$/i.test(t)) return true;
    if (/\/(?:sessions?|chats?|req-)/i.test(t)) return true;
    if (/^\d{6}\/\d{8}/.test(t)) return true;
    if (/^\d{8}$/.test(t)) return true;
    if (/^(heartbeat|system|cron\.execute)/i.test(t)) return true;
    return false;
  }

  function collapse(t) {
    return t.length > 56 ? t.slice(0, 53).replace(/\s+$/, '') + '\u2026' : t;
  }

  // ── Automations ──────────────────────────────────────────────────────────
  function automationActivity(job) {
    const name = (job && job.name || '').trim();
    const label = /briefing/i.test(name) ? briefingTitle(name) : collapse(name || 'Automation');
    return {
      title: label,
      meta: job && job.enabled ? 'Ran on schedule' : 'Last run',
      groupKey: 'automation:' + (name || ''),
    };
  }

  // ── Errors ───────────────────────────────────────────────────────────────
  function errorActivity(inc) {
    const channel = (inc && inc.channel || 'Ghost').replace(/[_-]/g, ' ');
    const msg = (inc && inc.last_error || '').trim();
    const title = msg ? capitalize(channel) + ': ' + msg : capitalize(channel) + ' error';
    const n = (inc && inc.failure_count) || 0;
    return { title, meta: n > 0 ? (n + ' failure' + (n === 1 ? '' : 's')) : 'Needs attention', groupKey: 'error:' + channel };
  }

  // ── Generic normalizer for an activity feed item ─────────────────────────
  // Given a raw internal item, produce { kind, ts, title, meta, groupKey }.
  // This is the single place activity semantics live, so Home and Activity do
  // not duplicate special-casing.
  function activityItem(it) {
    if (it.kind === 'conversation' || it.kind === 'messages') {
      return {
        kind: 'conversation',
        ts: it.ts,
        title: conversationTitle(it.title),
        meta: it.meta || '',
        groupKey: 'conversation:' + it.title,
      };
    }
    if (it.kind === 'automation' || it.kind === 'automations') {
      const a = automationActivity(it.job || { name: it.title, enabled: it.enabled });
      return { kind: 'automation', ts: it.ts, title: a.title, meta: it.meta || a.meta, groupKey: a.groupKey };
    }
    if (it.kind === 'memory') {
      const a = memoryActivity(it.file || (it.title ? { name: it.title, title: it.title } : {}));
      return { kind: 'memory', ts: it.ts, title: a.title, meta: it.meta || a.meta, groupKey: a.groupKey };
    }
    if (it.kind === 'error' || it.kind === 'errors') {
      const a = errorActivity(it.inc || it);
      return { kind: 'error', ts: it.ts, title: a.title, meta: it.meta || a.meta, groupKey: a.groupKey };
    }
    return { kind: it.kind || 'system', ts: it.ts, title: it.title || 'Something happened', meta: it.meta || '', groupKey: 'other:' + it.title };
  }

  // ── Grouping / deduplication ─────────────────────────────────────────────
  // Ordinal labels for repeated activity ("Discussed 3 times today").
  const COUNT_LABEL = ['', 'once', 'twice', 'three times', '4 times', '5 times', '6 times', '7 times', '8 times', '9 times', '10 times'];

  // groupItems collapses repeated activity within the same day into one row,
  // annotating how many times it happened, so a flood of repeated test
  // messages never overwhelms the timeline. History is never deleted — this is
  // purely the presentation of the feed.
  function groupItems(items) {
    const out = [];
    const byBucket = new Map();
    for (const it of items) {
      const day = dayKey(it.ts);
      const key = day + '|' + (it.groupKey || it.title);
      const bucket = byBucket.get(key);
      if (bucket) {
        bucket.count++;
        if (it.ts > bucket.ts) bucket.ts = it.ts;
      } else {
        const b = { ...it, count: 1 };
        byBucket.set(key, b);
        out.push(b);
      }
    }
    out.sort((a, b) => b.ts - a.ts);
    for (const it of out) {
      if (it.count > 1) {
        const n = it.count;
        const label = n <= 10 ? COUNT_LABEL[n] : (n + ' times');
        it.meta = it.meta ? it.meta + '  \u00b7  ' + label : label;
      }
      delete it.count;
    }
    return out;
  }

  function dayKey(ts) {
    if (!ts) return '0';
    const d = new Date(ts * 1000);
    return d.getFullYear() + '-' + (d.getMonth() + 1) + '-' + d.getDate();
  }

  // ── Memory (personal context) presentation ───────────────────────────────
  // domainTag renders "Domain · Kind" for a memory entry, e.g. "Food ·
  // Preference". It never exposes a raw predicate.
  function domainTag(domain, kind) {
    const d = domainWord(domain);
    const k = kindWord(kind, domain);
    // When the kind and domain refer to the same thing (Identity, People), the
    // tag reads better as a single word rather than "Identity · Identity".
    if (kind === 'relationship' && domain === 'relationship') return 'Relationship';
    if (kind === 'identity') return 'Identity';
    if (!d) return k;
    if (!k) return d;
    return d + ' \u00b7 ' + k;
  }

  const DOMAIN_WORD = {
    identity: 'Identity', food: 'Food', location: 'Location', work: 'Work',
    relationship: 'Relationship', routine: 'Routine', lifestyle: 'Lifestyle',
    communication: 'Communication', technology: 'Technology', health: 'Health',
    education: 'Education', entertainment: 'Entertainment', finance: 'Finance',
    family: 'Family', travel: 'Travel', other: 'Other',
  };
  const KIND_WORD = {
    identity: 'Identity', preference: 'Preference', fact: 'Fact', goal: 'Goal',
    relationship: 'People', routine: 'Routine', decision: 'Decision',
    consent: 'Consent', project: 'Project', constraint: 'Constraint', interest: 'Interest',
  };

  function domainWord(d) { return DOMAIN_WORD[d] || ''; }
  function kindWord(k, domain) {
    const w = KIND_WORD[k] || '';
    if (!w) return '';
    return w;
  }

  // inferDomainFromValue is a light heuristic used only when a stored domain is
  // too generic (e.g. "lifestyle") but the value clearly concerns a more
  // specific area. It reinforces the controlled vocabulary without inventing a
  // new one. Returns null when it cannot decide.
  function inferDomainFromValue(kind, domain, value) {
    const v = (value || '').toLowerCase();
    const foodWords = /(sushi|pizza|coffee|tea|food|dish|meal|drink|restaurant|pasta|burger|breakfast|dinner|lunch|beer|wine|cake|dessert|cook|recipe|eat|smoothie|juice|chocolate|noodle|ramen|taco|salad|steak)|\btea\b|\bcoffee\b/;
    if (kind === 'preference' && foodWords.test(v)) return 'food';
    if (kind === 'fact' && /(live in|lives in|city|country|bangkok|london|new york)/.test(v)) return 'location';
    return null;
  }

  // canonicalizeEntries collapses current memory entries that represent the
  // same fact (duplicate extractions, case/shape variants) so the Memory page
  // shows one canonical memory per belief. It never deletes anything.
  function canonicalizeEntries(entries) {
    const best = new Map(); // key -> best entry
    for (const e of entries) {
      const key = canonicalKey(e);
      if (!key) continue;
      const existing = best.get(key);
      if (!existing || betterEntry(e, existing)) best.set(key, e);
    }
    return [...best.values()];
  }

  function canonicalKey(e) {
    const kind = (e.kind || '').toLowerCase();
    const pred = (e.predicate || e.label || '').toLowerCase().replace(/[\/_]/g, ' ');
    const val = normValue(e.value || e.title || '');
    if (!kind || !val) return null;

    // "favorite food" and "favorite" (when the value is a food) are the same
    // belief; "prefers tea over coffee" is its own belief.
    const basePred = pred
      .replace(/favorite food/, 'favorite')
      .replace(/favorite drink/, 'favorite')
      .trim();
    return kind + '|' + basePred + '|' + val;
  }

  function normValue(s) {
    return String(s || '')
      .replace(/\.$/, '')
      .replace(/ and i are .*$/i, '')
      .replace(/[\u2018\u2019]/g, "'")
      .trim()
      .toLowerCase();
  }

  function betterEntry(a, b) {
    const ra = a.reinforce_count || 0, rb = b.reinforce_count || 0;
    if (ra !== rb) return ra > rb;
    const ca = a.confidence || 0, cb = b.confidence || 0;
    if (ca !== cb) return ca > cb;
    return (a.created_at || '') > (b.created_at || '');
  }

  // memoryTitleFor returns the canonical user-facing title for a current
  // personal-context entry. It prefers Ghost's stored title (already semantic),
  // otherwise derives one from kind+value.
  function memoryTitleFor(e) {
    const t = (e.title || '').trim();
    if (t && !/^(partner|colleague|family):\s*$/i.test(t)) return relationshipClean(t, e);
    return relationshipClean('', e) || titleFromKind(e);
  }

  // relationshipClean extracts just the person's name from a relationship value
  // that leaked the whole source sentence ("Sarah and I are business partners").
  function relationshipClean(title, e) {
    if ((e.kind || '').toLowerCase() !== 'relationship') return title;
    const val = (e.value || '').trim();
    let name = val;
    if (/( and i are | were | is my | is the )/i.test(val)) {
      const m = val.match(/^([^,.;]+?)(?:\s+and\s+i\s+are|\s+is\s+my|\s+is\s+the|\s+and\s+i\s+were)/i);
      if (m) name = m[1].trim();
    }
    name = String(name).replace(/\.+$/, '').trim();
    if (!name) return title || 'Relationship';
    const rel = relationshipWord(e.predicate || '');
    return rel + ': ' + name;
  }

  function relationshipWord(predicate) {
    const p = (predicate || '');
    if (/colleague|business|work/i.test(p)) return 'Colleague';
    if (/family|mother|father|sister|brother|parent/i.test(p)) return 'Family';
    return 'Partner';
  }

  function titleFromKind(e) {
    const val = (e.value || '').trim();
    const kind = (e.kind || '').toLowerCase();
    if (kind === 'goal') return 'Goal: ' + val;
    return val ? capitalize(val) : 'Memory';
  }

  // provenanceFor builds a short "why Ghost remembers this" sentence from
  // whatever provenance is actually stored. It never invents a source.
  function provenanceFor(e) {
    const refs = (e.sources || []).map(s => {
      const kind = (s.kind || '').replace(/_/g, ' ');
      if (kind === 'user declared') return 'you told Ghost';
      if (kind === 'user corrected') return 'you corrected it';
      if (kind === 'inferred') return 'Ghost inferred it from your conversations';
      return 'Ghost learned it';
    });
    const uniq = refs.filter((v, i) => refs.indexOf(v) === i);
    return uniq.length ? uniq.join('; ') : 'Ghost learned this from your conversations.';
  }

  return {
    decodeNote, isJournalNote, noteTitle, memoryActivity,
    conversationTitle, automationActivity, errorActivity,
    activityItem, groupItems, domainTag, inferDomainFromValue,
    canonicalizeEntries, memoryTitleFor, provenanceFor, humanDate, relativeDay,
  };
})();
