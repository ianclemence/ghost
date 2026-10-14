# Motion

Adapted from Emil Kowalski's design-engineering philosophy (emilkowalski/skills, MIT).
Taste is trained; details compound. Most of them go unnoticed, which is the point.

## 1. Should it animate at all?

Ask how often the person will see it.

| Frequency | Decision |
| --- | --- |
| Many times a minute (typing, list navigation, keyboard actions) | No animation |
| Tens of times a day (hover, tab switch) | Remove or shrink to almost nothing |
| Occasional (a sheet, a toast, a dialog) | Standard animation |
| Rare or first time (onboarding, a win screen) | Add delight |

Never animate an action that came from the keyboard. If the only reason is "it looks nice" and it repeats, leave it still.

## 2. What is it for?

Valid purposes: spatial consistency (it leaves the way it came), state change (a button becoming a check), explanation (showing how something works), feedback (pressing), softening a jump (content appearing or vanishing). Decoration is allowed only where it is rare.

## 3. Easing

Built-in `ease-in`, `ease-out`, `ease-in-out` are weak. Use the tokens already on the page.

```css
--ease-out:    cubic-bezier(0.23, 1, 0.32, 1);    /* entering, leaving, anything the user triggers */
--ease-in-out: cubic-bezier(0.77, 0, 0.175, 1);   /* things moving across the screen */
--ease-drawer: cubic-bezier(0.32, 0.72, 0, 1);    /* sheets and drawers */
```

- Entering or leaving: `--ease-out`. Moving on screen: `--ease-in-out`. Hover and colour: `ease`. Constant motion (a progress bar, a marquee): `linear`.
- Never `ease-in` for UI: it delays the first frames, which is exactly where the eye is looking.

## 4. Duration

| Element | Time |
| --- | --- |
| Press feedback | 100–160ms |
| Tooltip, small popover | 125–200ms |
| Dropdown, select | 150–250ms |
| Sheet, drawer, modal | 200–500ms |
| Explanatory or marketing | as long as the idea needs |

UI stays under 300ms. A 180ms menu feels faster than a 400ms one, and speed is perceived performance. Make exits faster than entrances. Be slow only where the person is deciding (a hold-to-confirm), fast where the system is responding.

## 5. Building blocks

**Press.** Everything pressable responds:

```css
.btn { transition: transform var(--fast) var(--ease-out); }
.btn:active { transform: scale(0.97); }
```

**Entrances.** Never from `scale(0)`; nothing in the world appears from nothing.

```css
.enter { opacity: 0; transform: translateY(8px) scale(0.98); }
.enter.in { opacity: 1; transform: none; transition: opacity var(--base) var(--ease-out), transform var(--base) var(--ease-out); }
```

Prefer CSS transitions to keyframes for anything that can be triggered repeatedly: a transition retargets mid-flight, a keyframe restarts from zero. Use `@starting-style` for entry animation where supported; otherwise toggle a class on the next frame.

**Origin.** Scale from where it came from: `transform-origin` at the trigger for popovers. Modals stay centred.

**Stagger.** When a group appears together, offset each item 30–80ms. Short. It is decoration: never block interaction while it plays.

```css
.item { opacity: 0; transform: translateY(8px); animation: rise 300ms var(--ease-out) forwards; animation-delay: calc(var(--i) * 50ms); }
@keyframes rise { to { opacity: 1; transform: none; } }
```

**Masking a rough crossfade.** If two states overlapping look wrong, add `filter: blur(2px)` during the swap (keep blur under 20px; it is costly).

**Reveals with `clip-path`.** `clip-path: inset(0 100% 0 0)` to `inset(0)` reveals left to right without extra elements. A hold-to-confirm is a coloured overlay that fills over 2s `linear` on press and snaps back in 200ms.

**Springs.** For things the finger drags and releases, momentum matters. In plain CSS use a `linear()` easing with an overshoot, or the Web Animations API; keep bounce subtle (the equivalent of 0.1–0.3).

**Drag.** Let a quick flick dismiss: judge by velocity, not only distance. Add friction instead of a hard stop at the edge. Capture the pointer so the drag continues outside the element. Ignore a second finger once a drag has begun.

## 6. Performance and access

- Animate `transform` and `opacity` only. Width, height, top, margin and padding re-layout every frame.
- CSS animations run off the main thread; use them for anything predetermined.
- Do not drive animation by writing CSS variables on a parent of many children.
- Respect `prefers-reduced-motion`: keep opacity and colour changes that aid understanding, remove movement.

```css
@media (prefers-reduced-motion: reduce) {
  .enter, .item { transform: none; animation: none; opacity: 1; }
  html { scroll-behavior: auto; }
}
```

- Gate hover animation: `@media (hover: hover) and (pointer: fine) { … }`. A tap triggers hover on touch screens and leaves it stuck.

## 7. Check it like a craftsperson

Slow it down: set durations to 4x and watch for two states overlapping, an easing that starts or stops abruptly, a wrong origin, properties that drift out of sync. Look again with fresh eyes before you call it done.
