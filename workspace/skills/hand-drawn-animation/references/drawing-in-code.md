# Drawing in code: stills, illustrations and filled characters

A drawing is a film with one frame. Start with `film new` using the `still`
template (a layered dusk scene with a fox, built with the method below), replace
the subject, and call `film still`. The same method draws the background of an
animation and the filled drawings of a character.

## The method

1. **Layers, back to front.** Sky wash, light source, far shapes, middle shapes,
   ground, subject, foreground marks, atmosphere. Each layer is lighter, bluer and
   lower in contrast the farther away it is. The subject has the strongest
   contrast and the warmest colour. A flat picture is usually a picture with one
   layer.
2. **Forms, not outlines.** Build a subject from a few overlapping filled shapes
   (an animal: body, haunch, chest, head, tail, legs, ears). Fill each, shade the
   side away from the light with hatching clipped to the shape, then ink it with
   a wobbling pressure line. `form()` in `still.html` does all three. Silhouette
   first: if the filled shapes alone read as the thing, the drawing works; if they
   do not, no amount of line work will save it.
3. **A limited palette.** Four to six colours and their tints. Put the subject's
   hue opposite the ground's (warm on cool). Name the colours in one object at the
   top of the file so a change of mood is one edit.
4. **One light.** Pick the side the light comes from and keep it: shade, cast
   shadow, rim light and the sun all agree.
5. **Scale and place.** The subject fills about a quarter of the frame width or
   more and sits on a thirds line. Use the whole frame: nothing important in a
   corner, no half of the picture blank.
6. **Seeded randomness.** `rng(seed)` and `noise1(x, seed)`, never `Math.random`,
   so every render of the file is the same picture.

## Shapes and strokes

| Want | Use |
|---|---|
| A smooth closed shape through points | `curvePath(pts, true, corner)`. `corner` is the sharpest turn (radians) still kept as a corner: `.8` keeps most corners (angular, cut-paper), `2.4` rounds almost everything (bodies, fur, clouds). |
| An irregular round shape (stone, cloud, bush) | `blob(cx, cy, rx, ry, seed, {amp, rot})` returns points |
| A hand-wobbled line with pressure | `wob(c, pts, amp, seed, close, {pressure:.6, corner})` strokes with the current `strokeStyle` and `lineWidth` |
| A line that fades or tapers | give `drawCel` strokes a `pressure` profile; for long lines use `[[0,.55],[.08,.95],[.92,.95],[1,.45]]` |
| Shading | hatching clipped to the form (`shadeHatch` in the template), or `hatch(c, path, box, opts)` |
| A gradient sky or ground | `c.createLinearGradient` with three stops; a radial gradient for a sun glow |
| Terrain | a polyline over `x` with `noise1(x / wavelength, seed) * amplitude`, closed along the bottom, filled |
| Texture on paper | `grain(c, path, box, n, color, alpha, seed)`, sparingly |

`cels.js` strokes have no fill: for a filled character draw the fills with
`c.fill(curvePath(...))` first and the cel's strokes on top.

## Recipes

- **An animal (sitting, side view).** Tail (a tapering crescent), haunch (a rounded
  oval), body (a larger oval tilted toward the head), chest (a lighter, narrower
  oval in front), legs (thin tapering shapes ending in a darker foot), head (a
  wedge: skull, tapering snout, a lighter muzzle), two ears (triangles with an
  inner darker triangle), a small dark eye with a highlight dot, a dark nose.
  Draw back to front so near forms overlap far ones. Add a soft cast shadow on the
  ground on the side away from the light.
- **A person or character.** Head (a rounded shape), torso, limbs as tapering
  forms. Keep proportions in one place (`const HEAD = 90;` and multiples) so a
  pose change does not change the character. Draw hands as simple mitten shapes
  unless the story needs fingers.
- **Trees.** Firs: stacked zig-zag tiers, snow on the light side of each tier.
  Broadleaf: a trunk, then several overlapping `blob` canopies, darker underneath.
  Vary height, spacing and tint with `rng`; two or three rows at different scales
  make a forest.
- **Water.** A flat wash, horizontal broken strokes of lighter and darker value,
  the reflection of each object a little darker and broken by ripples.
- **Buildings.** Rectangles with a perspective rule chosen once (one-point or
  two-point), windows from a loop, a few dark gaps for depth. A roofline against
  the sky does most of the work.
- **Sky and light.** A three-stop gradient warmest at the horizon, a radial glow
  around the sun or moon, clouds as a few `blob`s with a lighter top edge.
- **Atmosphere.** Falling snow or rain (bigger and brighter when nearer), mist as
  a low translucent band, a soft vignette (`createRadialGradient`) to hold the eye.

## Look and finish

Choose the finish from the brief. Clean filled shapes with an inked line (the
template) read as a children's-book illustration. For a pencil sketch draw with
`drawCel(..., {material: 'pencil'})` strokes only, light fills or none, visible
construction lines. For risograph or screen print use the plate helpers in
[style.md](style.md). A request for "a drawing" with no style named gets the
inked-and-filled look; a request for "a sketch" gets pencil.

## Common failures

- Filling in a cel (cels are strokes only): fill with the canvas, then draw strokes.
- Reading `W`, `H`, `CX`, `CY` at file scope in a film (a still's single scene may
  use them freely; any precomputed drawing may not). See the skill.
- Faceted, polygonal bodies: raise the `corner` argument of `curvePath` and `wob`.
- A tiny subject in a big empty frame, or everything in the top half.
- Hard black outlines on everything: ink the subject, let far shapes go without a
  line or use a tint of their fill.
- Every shape the same value: squint at the picture; if it is one grey, separate
  the layers by value first.
- A flat sky: always a gradient with a warm side.
