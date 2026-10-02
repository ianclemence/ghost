/* Ghost console wording: small pure helpers that decide how a stored thing is
   worded on screen. No DOM, so they can be tested without a browser. */
'use strict';

const GhostWording = (() => {
  // A comparable form of a sentence: case, spacing, an ellipsis and closing
  // punctuation do not make two wordings different.
  function norm(s) {
    return String(s || '')
      .replace(/…|\.\.\./g, '')
      .replace(/\s+/g, ' ')
      .trim()
      .replace(/[.!?;:,]+$/, '')
      .toLowerCase();
  }

  // routine decides what a routine shows as its name and what, if anything,
  // as the line under it. A routine stores a short title and the longer text
  // it was made from, and for a reminder they are usually the same words, so
  // showing both just says it twice. The second line appears only when it
  // adds something; and when the title was clipped with an ellipsis, the full
  // text becomes the title instead.
  function routine(title, what) {
    const t = String(title || '').trim();
    const w = String(what || '').trim();
    if (!w) return { title: t || 'Untitled', sub: '' };
    if (!t) return { title: w, sub: '' };
    const nt = norm(t);
    const nw = norm(w);
    if (nt === nw) return { title: t, sub: '' };
    const clipped = /(…|\.\.\.)\s*$/.test(t);
    if (clipped && nw.startsWith(nt)) return { title: w, sub: '' };
    return { title: t, sub: w };
  }

  return { norm, routine };
})();
