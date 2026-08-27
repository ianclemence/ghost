/* GhostQR — tiny dependency-free QR encoder (byte mode, EC level L, v1–10).
   Used for the device pairing invitation. Renders to a <canvas>.
   If anything fails, callers fall back to showing the raw pairing string. */
'use strict';

const GhostQR = (() => {
  // GF(256) with primitive polynomial 0x11d
  const EXP = new Array(256), LOG = new Array(256);
  (function () {
    let x = 1;
    for (let i = 0; i < 255; i++) {
      EXP[i] = x;
      LOG[x] = i;
      x <<= 1;
      if (x & 0x100) x ^= 0x11d;
    }
    EXP[255] = EXP[0];
  })();
  function gmul(a, b) { return a && b ? EXP[(LOG[a] + LOG[b]) % 255] : 0; }

  function rsGenerator(degree) {
    let poly = [1];
    for (let i = 0; i < degree; i++) {
      const next = new Array(poly.length + 1).fill(0);
      for (let j = 0; j < poly.length; j++) {
        next[j] ^= gmul(poly[j], 1);
        next[j + 1] ^= gmul(poly[j], EXP[i]);
      }
      poly = next;
    }
    return poly;
  }

  // Level L ec-codewords-per-block and block structure for versions 1–10.
  const EC = [
    null,
    { cw: 7,  groups: [[1, 19]] },            // v1
    { cw: 10, groups: [[1, 34]] },           // v2
    { cw: 15, groups: [[1, 55]] },           // v3
    { cw: 20, groups: [[1, 80]] },           // v4
    { cw: 26, groups: [[1, 108]] },          // v5
    { cw: 18, groups: [[2, 68]] },           // v6
    { cw: 20, groups: [[2, 78]] },           // v7
    { cw: 24, groups: [[2, 97]] },           // v8
    { cw: 30, groups: [[2, 116]] },          // v9
    { cw: 18, groups: [[2, 68], [2, 69]] },  // v10
  ];
  const ALIGN = [
    null, [], [6, 18], [6, 22], [6, 26], [6, 30], [6, 34],
    [6, 22, 38], [6, 24, 42], [6, 26, 46], [6, 28, 50],
  ];
  function totalDataCW(v) {
    let t = 0; const e = EC[v];
    for (const [n, cw] of e.groups) t += n * cw;
    return t;
  }

  function chooseVersion(len) {
    for (let v = 1; v <= 10; v++) {
      const cap = totalDataCW(v);
      const countBits = (v <= 9 ? 1 : 2);
      // reserve bytes for mode (1) + count + terminator (1)
      if (len <= cap - countBits - 2) return v;
    }
    return 0;
  }

  // 18-bit version information (BCH(18,6), generator 0x1F25) for versions 7–10.
  function versionInfoBits(v) {
    let rem = v << 12;
    for (let i = 17; i >= 12; i--) {
      if ((rem >> i) & 1) rem ^= 0x1F25 << (i - 12);
    }
    return (v << 12) | (rem & 0xFFF);
  }

  function bitBuffer() {
    const bytes = []; let cur = 0, n = 0;
    return {
      put(val, len) { for (let i = len - 1; i >= 0; i--) { cur = (cur << 1) | ((val >> i) & 1); n++; if (n === 8) { bytes.push(cur); cur = 0; n = 0; } } },
      bytes() { if (n > 0) { cur <<= (8 - n); bytes.push(cur); } return bytes; },
    };
  }

  function encodeData(text, v) {
    const buf = bitBuffer();
    buf.put(0x04, 4); // byte mode
    const len = text.length;
    buf.put(len, v <= 9 ? 8 : 16);
    for (let i = 0; i < len; i++) buf.put(text.charCodeAt(i) & 0xff, 8);
    buf.put(0x00, 4); // terminator
    let bytes = buf.bytes();
    const need = totalDataCW(v);
    while (bytes.length < need) bytes.push(0xec);
    // pad alternating ec11 ec
    let pad = 0xec;
    while (bytes.length < need) { bytes.push(pad); pad = pad === 0xec ? 0x11 : 0xec; }

    // Interleave with Reed-Solomon
    const e = EC[v];
    const ecCW = e.cw;
    const blocks = [];
    let idx = 0;
    for (const [count, dataLen] of e.groups) {
      for (let b = 0; b < count; b++) {
        const data = bytes.slice(idx, idx + dataLen); idx += dataLen;
        const gen = rsGenerator(ecCW);
        const ec = new Array(ecCW).fill(0);
        for (const d of data) {
          const factor = d ^ ec[0];
          ec.shift();
          ec.push(0);
          if (factor !== 0) for (let i = 0; i < ecCW; i++) ec[i] ^= gmul(gen[i + 1], factor);
        }
        blocks.push({ data, ec });
      }
    }
    const result = [];
    const maxData = Math.max(...blocks.map(b => b.data.length));
    for (let i = 0; i < maxData; i++) for (const b of blocks) if (i < b.data.length) result.push(b.data[i]);
    for (let i = 0; i < ecCW; i++) for (const b of blocks) result.push(b.ec[i]);
    return result;
  }

  function buildMatrix(v, data) {
    const size = v * 4 + 17;
    const m = Array.from({ length: size }, () => new Array(size).fill(null));
    // finder patterns
    function finder(r, c) {
      for (let i = -1; i <= 7; i++) for (let j = -1; j <= 7; j++) {
        const rr = r + i, cc = c + j;
        if (rr < 0 || rr >= size || cc < 0 || cc >= size) continue;
        const inb = i >= 0 && i <= 6 && j >= 0 && j <= 6;
        const ring = (i === 0 || i === 6 || j === 0 || j === 6);
        const core = (i >= 2 && i <= 4 && j >= 2 && j <= 4);
        m[rr][cc] = (inb && (ring || core)) ? 1 : (inb ? 0 : m[rr][cc]);
      }
    }
    finder(0, 0); finder(0, size - 7); finder(size - 7, 0);
    // separators already handled by finder bounds
    // timing patterns
    for (let i = 8; i < size - 8; i++) {
      if (m[i][6] === null) m[i][6] = (i % 2 === 0) ? 1 : 0;
      if (m[6][i] === null) m[6][i] = (i % 2 === 0) ? 1 : 0;
    }
    // alignment patterns
    const pos = ALIGN[v];
    if (pos && pos.length) {
      for (const r of pos) for (const c of pos) {
        if ((r <= 7 && c <= 7) || (r <= 7 && c >= size - 8) || (r >= size - 8 && c <= 7)) continue;
        for (let i = -2; i <= 2; i++) for (let j = -2; j <= 2; j++) {
          const ring = (Math.abs(i) === 2 || Math.abs(j) === 2);
          const core = (i === 0 && j === 0);
          m[r + i][c + j] = (ring || core) ? 1 : 0;
        }
      }
    }
    // dark module + format reservation
    m[size - 8][8] = 1;
    // format info area left as null; we fill after data
    // reserve + write version info area (v>=7)
    if (v >= 7) {
      const info = versionInfoBits(v);
      for (let i = 0; i < 6; i++) for (let j = 0; j < 3; j++) {
        m[i][size - 11 + j] = 0; m[size - 11 + j][i] = 0;
      }
      for (let i = 0; i < 18; i++) {
        const bit = (info >> i) & 1;
        const a = Math.floor(i / 3), b = i % 3;
        m[a][size - 11 + b] = bit;
        m[size - 11 + b][a] = bit;
      }
    }
    // place data
    let di = 0;
    const getBit = () => { const b = data[di >> 3]; return (b >> (7 - (di & 7))) & 1; };
    let up = true; let col = size - 1;
    while (col > 0) {
      if (col === 6) col--; // skip timing
      for (let i = 0; i < size; i++) {
        const r = up ? size - 1 - i : i;
        for (let c = 0; c < 2; c++) {
          const cc = col - c;
          if (m[r][cc] === null) {
            let bit = 0;
            if (di < data.length * 8) { bit = getBit(); di++; }
            m[r][cc] = bit;
          }
        }
      }
      up = !up; col -= 2;
    }
    // masks
    function applyMask(mask) {
      const mm = m.map(row => row.slice());
      for (let r = 0; r < size; r++) for (let c = 0; c < size; c++) {
        if (mm[r][c] === null) continue;
        let flip = false;
        switch (mask) {
          case 0: flip = (r + c) % 2 === 0; break;
          case 1: flip = r % 2 === 0; break;
          case 2: flip = c % 3 === 0; break;
          case 3: flip = (r + c) % 3 === 0; break;
          case 4: flip = (Math.floor(r / 2) + Math.floor(c / 3)) % 2 === 0; break;
          case 5: flip = (r * c) % 2 + (r * c) % 3 === 0; break;
          case 6: flip = ((r * c) % 2 + (r * c) % 3) % 2 === 0; break;
          case 7: flip = ((r + c) % 2 + (r * c) % 3) % 2 === 0; break;
        }
        if (flip) mm[r][c] ^= 1;
      }
      return mm;
    }
    // format info (EC level L = 01, mask pattern)
    function formatBits(mask) {
      let data = (0b01 << 3) | mask; // 5 bits
      const g = rsGenerator(10);
      let rem = data << 10;
      const denom = 0b10100110111;
      for (let i = 14; i >= 10; i--) { if ((rem >> i) & 1) rem ^= denom << (i - 10); }
      let bits = ((data << 10) | rem) ^ 0b101010000010010;
      const out = [];
      for (let i = 0; i < 15; i++) out.push((bits >> i) & 1);
      return out;
    }
    let best = null, bestPenalty = 1e9;
    for (let mask = 0; mask < 8; mask++) {
      const mm = applyMask(mask);
      const fb = formatBits(mask);
      // place format
      const place = (r, c, bit) => { mm[r][c] = bit; };
      for (let i = 0; i < 15; i++) {
        const bit = fb[i];
        if (i < 6) place(i, 8, bit); else if (i < 8) place(i + 1, 8, bit); else if (i < 9) place(size - 1 - (i - 8), 8, bit); else place(8, i - 9, bit);
        const j = i < 8 ? 7 - i : 14 - i;
        if (j < 7) place(8, j, bit); else place(8, size - 15 + j, bit);
      }
      // penalty (rough)
      let pen = 0;
      for (let r = 0; r < size; r++) { let run = 1; for (let c = 1; c < size; c++) { if (mm[r][c] === mm[r][c - 1]) { run++; } else { if (run >= 5) pen += run - 2; run = 1; } } }
      for (let c = 0; c < size; c++) { let run = 1; for (let r = 1; r < size; r++) { if (mm[r][c] === mm[r - 1][c]) { run++; } else { if (run >= 5) pen += run - 2; run = 1; } } }
      if (pen < bestPenalty) { bestPenalty = pen; best = mm; }
    }
    return best;
  }

  function draw(text, canvas, scale) {
    scale = scale || 5;
    const v = chooseVersion(text.length);
    if (!v) return false;
    const data = encodeData(text, v);
    const m = buildMatrix(v, data);
    if (!m) return false;
    const size = m.length;
    const quiet = 4;
    const dim = (size + quiet * 2) * scale;
    canvas.width = dim; canvas.height = dim;
    const ctx = canvas.getContext('2d');
    ctx.fillStyle = '#ffffff'; ctx.fillRect(0, 0, dim, dim);
    ctx.fillStyle = '#1c1b19';
    for (let r = 0; r < size; r++) for (let c = 0; c < size; c++) {
      if (m[r][c]) ctx.fillRect((c + quiet) * scale, (r + quiet) * scale, scale, scale);
    }
    return true;
  }

  return { draw };
})();
