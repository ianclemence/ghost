/* Ghost Motion player. Draws a motion spec at any moment t, the same way every
 * time: the phone previews it with it, the Pod renders it frame by frame into an
 * MP4 with it. Nothing here is random and nothing depends on wall-clock time
 * except play(), which only advances t.
 *
 * window.ghostMotion = { duration, seek(t), play(), pause(), ready }
 */
(function () {
  "use strict";
  var spec = window.__MOTION__;
  var mode = window.__MOTION_MODE__ || "preview"; // "preview" | "render"
  var W = spec.size === "landscape" ? 1280 : spec.size === "square" ? 1080 : 720;
  var H = spec.size === "landscape" ? 720 : spec.size === "square" ? 1080 : 1280;
  var U = Math.min(W, H) / 100; // one unit: 1% of the short side
  var accent = spec.accent || "#9C95FF";

  // ── easing ─────────────────────────────────────────────────────────────
  function clamp(x, a, b) { return x < a ? a : x > b ? b : x; }
  function outCubic(x) { x = clamp(x, 0, 1); return 1 - Math.pow(1 - x, 3); }
  function outQuint(x) { x = clamp(x, 0, 1); return 1 - Math.pow(1 - x, 5); }
  function inOut(x) { x = clamp(x, 0, 1); return x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2; }
  function prog(t, start, dur) { return clamp((t - start) / dur, 0, 1); }

  // ── formatting ─────────────────────────────────────────────────────────
  function fmt(v, dec) {
    var n = Number(v);
    var abs = Math.abs(n);
    var s;
    if (dec === undefined || dec === null) dec = abs >= 100 || Number.isInteger(n) ? 0 : 1;
    s = abs.toFixed(dec);
    var parts = s.split(".");
    parts[0] = parts[0].replace(/\B(?=(\d{3})+(?!\d))/g, ",");
    return (n < 0 ? "−" : "") + parts.join(".");
  }
  function short(v) {
    var a = Math.abs(v);
    if (a >= 1e9) return fmt(v / 1e9, 1) + "B";
    if (a >= 1e6) return fmt(v / 1e6, 1) + "M";
    if (a >= 1e4) return fmt(v / 1e3, 0) + "k";
    return fmt(v);
  }
  function hexA(hex, a) {
    var n = parseInt(hex.slice(1), 16);
    return "rgba(" + ((n >> 16) & 255) + "," + ((n >> 8) & 255) + "," + (n & 255) + "," + a + ")";
  }
  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  var SVG = "http://www.w3.org/2000/svg";
  function svg(tag, attrs) {
    var e = document.createElementNS(SVG, tag);
    for (var k in attrs) e.setAttribute(k, attrs[k]);
    return e;
  }

  // ── stage ──────────────────────────────────────────────────────────────
  var root = document.getElementById("motion");
  root.style.setProperty("--u", U + "px");
  root.style.setProperty("--accent", accent);
  root.style.width = W + "px";
  root.style.height = H + "px";
  var glow = el("div", "glow");
  root.appendChild(glow);
  var glow2 = el("div", "glow two");
  root.appendChild(glow2);

  // Scene start times.
  var starts = [];
  var total = 0;
  spec.scenes.forEach(function (s) { starts.push(total); total += s.duration; });

  var FADE = 0.45; // scene crossfade

  // Each element becomes a node plus an update(t) that sets its look at local
  // time t (seconds since the element entered).
  function build(e, sceneDur) {
    var node, up;
    var stay = e.stay ? e.stay : sceneDur - (e.at || 0);
    switch (e.type) {
      case "title": {
        node = el("div", "e title");
        var h = el("div", "title-text", e.text);
        node.appendChild(h);
        var sub = e.sub ? el("div", "title-sub", e.sub) : null;
        if (sub) node.appendChild(sub);
        up = function (t) {
          var p = outQuint(prog(t, 0, 0.9));
          h.style.opacity = p;
          h.style.transform = "translateY(" + (1 - p) * 4 * U + "px)";
          h.style.filter = "blur(" + (1 - p) * 0.8 * U + "px)";
          if (sub) {
            var q = outCubic(prog(t, 0.35, 0.8));
            sub.style.opacity = q;
            sub.style.transform = "translateY(" + (1 - q) * 2 * U + "px)";
          }
        };
        break;
      }
      case "text":
      case "quote": {
        node = el("div", "e " + e.type);
        var tx = el("div", e.type === "quote" ? "quote-text" : "text-text", e.type === "quote" ? "“" + e.text + "”" : e.text);
        node.appendChild(tx);
        var by = e.sub ? el("div", "quote-by", e.type === "quote" ? "— " + e.sub : e.sub) : null;
        if (by) node.appendChild(by);
        up = function (t) {
          var p = outCubic(prog(t, 0, 0.8));
          tx.style.opacity = p;
          tx.style.transform = "translateY(" + (1 - p) * 2.5 * U + "px)";
          if (by) by.style.opacity = outCubic(prog(t, 0.5, 0.7));
        };
        break;
      }
      case "number": {
        node = el("div", "e number");
        var num = el("div", "number-value");
        node.appendChild(num);
        var lab = e.label ? el("div", "number-label", e.label) : null;
        if (lab) node.appendChild(lab);
        var from = e.from || 0, to = e.to || 0;
        var count = Math.min(1.6, Math.max(0.8, stay * 0.45));
        up = function (t) {
          var p = outQuint(prog(t, 0.1, count));
          num.textContent = (e.prefix || "") + fmt(from + (to - from) * p, e.decimals) + (e.suffix || "");
          var a = outCubic(prog(t, 0, 0.5));
          num.style.opacity = a;
          num.style.transform = "scale(" + (0.94 + 0.06 * a) + ")";
          if (lab) {
            var q = outCubic(prog(t, 0.4, 0.6));
            lab.style.opacity = q;
            lab.style.transform = "translateY(" + (1 - q) * 1.5 * U + "px)";
          }
        };
        break;
      }
      case "bars": {
        node = el("div", "e bars");
        if (e.label) node.appendChild(el("div", "chart-label", e.label));
        var max = Math.max.apply(null, e.values.map(Math.abs)) || 1;
        var rows = e.values.map(function (v, i) {
          var r = el("div", "bar-row");
          var l = el("div", "bar-name", e.labels[i]);
          var track = el("div", "bar-track");
          var fill = el("div", "bar-fill");
          if (i === e.values.indexOf(Math.max.apply(null, e.values))) fill.classList.add("lead");
          track.appendChild(fill);
          var val = el("div", "bar-value", "");
          r.appendChild(l); r.appendChild(track); r.appendChild(val);
          node.appendChild(r);
          return { r: r, fill: fill, val: val, v: v };
        });
        up = function (t) {
          rows.forEach(function (o, i) {
            var s = 0.15 + i * Math.min(0.18, 1.2 / rows.length);
            var p = outQuint(prog(t, s, 0.9));
            o.r.style.opacity = outCubic(prog(t, s - 0.1, 0.4));
            o.fill.style.width = (Math.abs(o.v) / max) * 100 * p + "%";
            o.val.textContent = short(o.v * p) + (e.unit ? " " + e.unit : "");
          });
        };
        break;
      }
      case "line": {
        node = el("div", "e line");
        if (e.label) node.appendChild(el("div", "chart-label", e.label));
        // Drawn at its real pixel size, so the line is not stretched and its
        // dash (how much of it is drawn) is measured in the same units.
        var vw = W - 16 * U, vh = 34 * U;
        var s = svg("svg", { viewBox: "0 0 " + vw + " " + vh, width: vw, height: vh, class: "line-svg" });
        var lo = Math.min.apply(null, e.values), hi = Math.max.apply(null, e.values);
        if (hi === lo) { hi += 1; lo -= 1; }
        var pts = e.values.map(function (v, i) {
          return [i / (e.values.length - 1) * vw, vh - U - ((v - lo) / (hi - lo)) * (vh - 2 * U)];
        });
        var d = pts.map(function (p, i) { return (i ? "L" : "M") + p[0].toFixed(2) + " " + p[1].toFixed(2); }).join(" ");
        var area = svg("path", { d: d + " L" + vw + " " + vh + " L0 " + vh + " Z", class: "line-area" });
        var path = svg("path", { d: d, class: "line-path" });
        s.appendChild(area); s.appendChild(path);
        node.appendChild(s);
        var ends = el("div", "line-ends");
        var first = el("div", "", e.labels[0]);
        var last = el("div", "line-last");
        ends.appendChild(first); ends.appendChild(last);
        node.appendChild(ends);
        var len = 0;
        up = function (t) {
          if (!len) { try { len = path.getTotalLength() || 300; } catch (x) { len = 300; } path.style.strokeDasharray = len; }
          var p = inOut(prog(t, 0.15, Math.min(2, stay * 0.6)));
          path.style.strokeDashoffset = len * (1 - p);
          area.style.opacity = 0.9 * outCubic(prog(t, 0.9, 1));
          node.style.opacity = outCubic(prog(t, 0, 0.4));
          last.textContent = e.labels[e.labels.length - 1] + "  " + short(e.values[e.values.length - 1] * (0.6 + 0.4 * p)) + (e.unit ? " " + e.unit : "");
          last.style.opacity = outCubic(prog(t, 1.2, 0.5));
        };
        break;
      }
      case "donut": {
        node = el("div", "e donut");
        var sum = e.values.reduce(function (a, b) { return a + b; }, 0) || 1;
        var ds = svg("svg", { viewBox: "0 0 100 100", class: "donut-svg" });
        var R = 38, C = 2 * Math.PI * R;
        ds.appendChild(svg("circle", { cx: 50, cy: 50, r: R, class: "donut-track" }));
        var segs = [], acc = 0;
        e.values.forEach(function (v, i) {
          var c = svg("circle", { cx: 50, cy: 50, r: R, class: "donut-seg", transform: "rotate(-90 50 50)" });
          c.style.stroke = i === 0 ? accent : "rgba(255,255,255," + (0.55 - i * 0.09) + ")";
          ds.appendChild(c);
          segs.push({ c: c, start: acc / sum, share: v / sum });
          acc += v;
        });
        node.appendChild(ds);
        var legend = el("div", "donut-legend");
        e.labels.forEach(function (l, i) {
          var row = el("div", "legend-row");
          var dot = el("span", "dot");
          dot.style.background = i === 0 ? accent : "rgba(255,255,255," + (0.55 - i * 0.09) + ")";
          row.appendChild(dot);
          row.appendChild(el("span", "", l + "  " + Math.round((e.values[i] / sum) * 100) + "%"));
          legend.appendChild(row);
        });
        node.appendChild(legend);
        up = function (t) {
          var p = inOut(prog(t, 0.1, 1.4));
          segs.forEach(function (o) {
            var visible = clamp(p - o.start, 0, o.share);
            o.c.style.strokeDasharray = (visible * C) + " " + C;
            o.c.style.strokeDashoffset = -o.start * C;
          });
          legend.style.opacity = outCubic(prog(t, 0.9, 0.6));
          node.style.opacity = outCubic(prog(t, 0, 0.4));
        };
        break;
      }
      case "list":
      case "steps": {
        node = el("div", "e " + e.type);
        if (e.label) node.appendChild(el("div", "chart-label", e.label));
        var gap = Math.min(0.5, Math.max(0.18, (stay - 1) / Math.max(1, e.items.length)));
        var items = e.items.map(function (it, i) {
          var r = el("div", "item");
          var mark = el("div", "mark", e.type === "steps" ? String(i + 1) : "");
          r.appendChild(mark);
          r.appendChild(el("div", "item-text", it));
          node.appendChild(r);
          return { r: r, mark: mark };
        });
        up = function (t) {
          items.forEach(function (o, i) {
            var s = 0.15 + i * gap;
            var p = outCubic(prog(t, s, 0.55));
            o.r.style.opacity = e.type === "steps" ? 0.28 + 0.72 * p : p;
            o.r.style.transform = "translateY(" + (1 - p) * 2 * U + "px)";
            if (e.type === "steps") {
              var on = t >= s && (i === items.length - 1 || t < 0.15 + (i + 1) * gap);
              o.mark.classList.toggle("on", on || (t >= s && i === items.length - 1));
              o.mark.classList.toggle("done", t >= 0.15 + (i + 1) * gap && i < items.length - 1);
            }
          });
        };
        break;
      }
      case "compare": {
        node = el("div", "e compare");
        if (e.label) node.appendChild(el("div", "chart-label", e.label));
        var cmax = Math.max(Math.abs(e.values[0]), Math.abs(e.values[1])) || 1;
        var cols = el("div", "cmp-cols");
        var parts = e.values.map(function (v, i) {
          var c = el("div", "cmp-col" + (i === 1 ? " lead" : ""));
          var big = el("div", "cmp-value");
          var bar = el("div", "cmp-bar");
          c.appendChild(big); c.appendChild(bar);
          c.appendChild(el("div", "cmp-name", e.labels[i]));
          cols.appendChild(c);
          return { big: big, bar: bar, v: v };
        });
        node.appendChild(cols);
        up = function (t) {
          parts.forEach(function (o, i) {
            var p = outQuint(prog(t, 0.2 + i * 0.45, 1));
            o.bar.style.height = (Math.abs(o.v) / cmax) * 18 * U * p + "px";
            o.big.textContent = (e.prefix || "") + short(o.v * p) + (e.suffix || "");
            o.big.style.opacity = outCubic(prog(t, 0.1 + i * 0.45, 0.5));
          });
        };
        break;
      }
    }
    return { node: node, up: up, at: e.at || 0, stay: stay };
  }

  var scenes = spec.scenes.map(function (s, i) {
    var wrap = el("div", "scene");
    var stack = el("div", "stack");
    wrap.appendChild(stack);
    var parts = s.elements.map(function (e) {
      var b = build(e, s.duration);
      stack.appendChild(b.node);
      return b;
    });
    var cap = s.caption ? el("div", "caption", s.caption) : null;
    if (cap) wrap.appendChild(cap);
    root.appendChild(wrap);
    return { wrap: wrap, parts: parts, cap: cap, start: starts[i], dur: s.duration, last: i === spec.scenes.length - 1 };
  });

  var bar = null, playBtn = null;
  if (mode === "preview") {
    bar = el("div", "progress");
    bar.appendChild(el("div", "progress-fill"));
    root.appendChild(bar);
    playBtn = el("div", "play");
    root.appendChild(playBtn);
  }

  var now = 0;
  function seek(t) {
    now = clamp(t, 0, total);
    scenes.forEach(function (s) {
      var local = now - s.start;
      var inScene = local >= -FADE && local <= s.dur + (s.last ? 1 : 0);
      if (!inScene) { s.wrap.style.display = "none"; return; }
      s.wrap.style.display = "flex";
      // Crossfade: in over the first FADE, out over the last FADE (not the last scene).
      var a = outCubic(prog(local, 0, FADE));
      if (!s.last) a *= 1 - inOut(prog(local, s.dur - FADE, FADE));
      s.wrap.style.opacity = a;
      s.parts.forEach(function (p) {
        var lt = local - p.at;
        if (lt < 0) { p.node.style.visibility = "hidden"; p.up(0); return; }
        p.node.style.visibility = "visible";
        var fadeOut = p.at + p.stay < s.dur ? 1 - outCubic(prog(local, p.at + p.stay, 0.4)) : 1;
        p.node.style.opacity = fadeOut;
        p.up(lt);
      });
      if (s.cap) s.cap.style.opacity = outCubic(prog(local, 0.6, 0.8));
    });
    // The light behind drifts slowly, by t alone.
    var g = now / Math.max(total, 1);
    glow.style.transform = "translate(" + (Math.sin(now * 0.35) * 6 * U) + "px," + (-g * 10 * U) + "px)";
    glow2.style.transform = "translate(" + (Math.cos(now * 0.28) * 8 * U) + "px," + (Math.sin(now * 0.2) * 5 * U) + "px)";
    if (bar) bar.firstChild.style.width = (now / total) * 100 + "%";
  }

  var playing = false, t0 = 0, raf = 0;
  function frame(ts) {
    if (!playing) return;
    var t = (ts - t0) / 1000;
    if (t >= total) { seek(total); playing = false; root.classList.add("ended"); return; }
    seek(t);
    raf = requestAnimationFrame(frame);
  }
  function play() {
    if (playing) return;
    if (now >= total) now = 0;
    root.classList.remove("ended", "paused");
    playing = true;
    t0 = performance.now() - now * 1000;
    raf = requestAnimationFrame(frame);
  }
  function pause() { playing = false; cancelAnimationFrame(raf); root.classList.add("paused"); }

  var ready = (document.fonts && document.fonts.ready ? document.fonts.ready : Promise.resolve()).then(function () { seek(0); });
  window.ghostMotion = { duration: total, width: W, height: H, seek: seek, play: play, pause: pause, ready: ready };

  if (mode === "render") {
    var fr = document.getElementById("frame");
    fr.style.width = W + "px";
    fr.style.height = H + "px";
  }
  if (mode === "preview") {
    // The stage fills the width of the window it runs in.
    // Inline in the chat it stays under the window's height; full screen it
    // fits the whole screen. Either way it is centred on the dark ground.
    function fit() {
      var cw = document.documentElement.clientWidth;
      var ch = window.__GHOST_INLINE__ ? 480 : document.documentElement.clientHeight || window.innerHeight;
      var s = Math.min(cw / W, ch / H);
      var left = Math.max(0, (cw - W * s) / 2);
      root.style.transform = "translate(" + left + "px,0) scale(" + s + ")";
      document.getElementById("frame").style.height = (window.__GHOST_INLINE__ ? H * s : Math.max(H * s, ch)) + "px";
      if (!window.__GHOST_INLINE__) root.style.top = Math.max(0, (ch - H * s) / 2) + "px";
    }
    fit();
    window.addEventListener("resize", fit);
    root.addEventListener("click", function () { playing ? pause() : play(); });
    ready.then(function () { setTimeout(play, 350); });
    window.addEventListener("message", function (ev) {
      var m = ev && ev.data;
      if (m === "motion:play") play();
      if (m === "motion:pause") pause();
    });
  }
})();
