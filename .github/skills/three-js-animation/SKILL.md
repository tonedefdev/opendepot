---
name: three-js-animation
description: "Use when: building, retiming, or reviewing a three.js explainer or promo animation rendered to video; designing scene choreography, camera moves, robotic arm or conveyor motion, on-screen captions, or end cards; setting up the HTML-to-MP4 render pipeline with window.renderAt, stills, and ffmpeg; or diagnosing rushed pacing, empty frames, camera drift, or floating objects in a rendered animation. Triggers: 'promo video', 'explainer animation', 'three.js scene', 'render the video', 'keyframes', 'camera move', 'pacing feels rushed', 'stills', 'conveyor', 'robot arm animation'."
argument-hint: "What the animation must explain, e.g. 'CLI hook verifies skills before load' or 'camera pan on conveyor beat'"
---

# Three.js Animation for Explainer Videos

This skill captures what worked and what did not while building the OpenDepot feature explainer in `promo/feature/index.html`. Use it as the default playbook for any three.js piece that will be rendered to a video.

Background: the `threejs-*` user skills cover three.js APIs. This skill covers the decisions that make an animation read as a story, and the pipeline that turns it into a reviewable MP4.

## 1. Start from the message, not the scene

- Write the one-sentence claim the video must land before touching geometry. For the OpenDepot feature it was: "the `opendepot` CLI, run as a harness hook, verifies every installed skill before an agent loads it."
- Split the claim into beats. Each beat gets one visual action, one caption, and one camera intent.
- Ground all copy in real behaviour. Capture real command output (for example, `opendepot verify`) and quote it exactly. Do not invent CLI wording, flags, or outcomes.
- Let imagery carry metaphor. The copy should describe what actually happens. A drill can stand in for an edit, but the caption should say the edit was rejected, not that a drill was used.

## 2. Pacing: take the time the explanation needs

- Do not time-box for a quick hook. Viewers need time to read a caption and understand what they are seeing. Feedback on this project was consistent: scenes that felt rushed were the main problem.
- Give every explanatory beat a hold long enough to read the caption twice, plus time for the visual to resolve.
- A long quiet stretch is a failure if nothing moves. If the viewer is waiting, fill the time with real activity that supports the narration: more items arriving, an arm working, a belt in motion, a status readout updating.
- Use a global time scale when pacing needs changing, instead of retiming every keyframe. In the feature scene this is `TIME_SCALE`, applied in `renderAt`. Author in scene seconds and scale once at the boundary.

## 3. Choreography rules

- Keep the subject in frame. If a beat is about a failed edit, the failed edit stays visible until the next beat starts. Do not move the camera to something unrelated mid-beat.
- Never let objects appear from nowhere. Spawn things off-screen and move them in on a belt or path. In the feature scene, arrivals spawn at `T - 0.5 - (PICK_X - BELT_START) / BELT_V`, so each item reaches the pick point at its scheduled time.
- Keep names legible on objects that stay on screen. A label that disappears the moment an object is stacked loses the reader.
- Show the outcome of every action. A reject needs a visible reject mark, and a load needs a visible load. Do not leave the viewer to infer the result from the caption alone.
- Stage one action at a time, then cut or pan once it resolves.

## 4. Camera

- The camera path is a `CatmullRomCurve3` sampled by an index-based `camIndex(t)`. The index maps time linearly within each key segment, so the spline is not evenly timed. Adding or removing a key shifts the path, so edit keys in place when possible.
- Identical neighbouring keys cause drift. A Catmull-Rom segment between two keys with the same pose still bulges toward the adjacent keys. The fix was `CAM_HOLD`, which pins the index to one key across a hold range. Use this whenever a shot must stay still.
- Check hold boundaries explicitly. Render stills at the start, middle, and end of every hold and every pan. A pan that starts before the subject has resolved reads as a bug.
- Add a small sinusoidal handheld offset to the position only. Keep it under about 0.02 units so it does not undermine framing.
- Keep the focus target near the subject. If the look target sits well off the subject, the subject drifts out of frame even when the position is correct.

## 5. Robotic arms and mechanisms

- Use two-bone IK toward a target, with keys sorted by time. Out-of-order `ARM_KEYS` produce snapping. `sampleArm` should interpolate between sorted keys.
- Morphs (for example, a claw becoming a drill) should be time-bounded and reversible. Tie the morph to the same beat as the caption so the image leads and the copy grounds it.
- Hide objects below a scale threshold, or give tools a minimum visible scale. Sub-pixel specks look like rendering noise, and this was a repeated complaint.

## 6. Conveyors, stacks, and cargo

- Rotate cargo around its own pivot, not around the belt origin. A box that drifts or tilts while riding the belt looks broken.
- Alternate stacking positions by row and column so the rack fills in a readable pattern. Do not stack everything at one point.
- Keep the belt busy with arrivals while the arm works. Empty belts read as dead time.

## 7. Overlays and captions

- Use HTML overlays positioned over the canvas for captions, tags, and terminal panels. They render crisply and are easy to edit.
- Drive captions from one ordered script (`capScript`) with start and end times, a headline, and an optional paragraph. An empty paragraph hides the paragraph element.
- Keep captions readable: headline above the scene, high contrast, no more than two lines of headline, and one idea per caption.
- For terminal output panels, use monospace text that matches the real CLI output exactly.

## 8. Brand and palette

- Follow the `opendepot-branding` skill for palette and typography. Core colours are `#000000`, `#0350C7`, `#03DEB8`, and `#FFFFFF`. The approved gradient midpoint is `#048FC3`.
- On dark scenes, use the white `Open` and mint `Depot` wordmark. The light-background logo does not work on dark scenes.
- Alarm red for rejections sits outside the official palette. Use it sparingly, and treat it as a state colour, not a brand colour.
- Aspekta may not be installed locally. Confirm the fallback font renders acceptably before reviewing stills.

## 9. three.js r186 gotchas

- Use `THREE.PCFShadowMap`. `PCFSoftShadowMap` was removed.
- `UnrealBloomPass` threshold near `0.95` keeps only hot highlights bloomed. Lower values bloom the whole scene.
- Avoid `MeshPhysicalMaterial` transmission for glass-like cubes. It produced speckle. Use a transparent material with `depthWrite: false` instead.
- Use `ACESFilmicToneMapping`. Keep exposure near `1.0` and adjust lights to change mood.

## 10. Render pipeline

The reference harness is `promo/agents/render.mjs`, which takes `--scene` and `--stills`. The older `promo/render.mjs` renders only the root `promo/index.html` and has no scene flag. Do not modify either for a new scene.

- Scene contract: the HTML page exposes `window.DURATION` (output seconds) and `window.renderAt(outputSeconds)`, which sets the scene to that frame.
- Full render: `cd promo && node agents/render.mjs --scene feature`. The harness writes `out/opendepot-feature.mp4` at 30 fps. A 93 s, 2790-frame render takes roughly 15–20 minutes. Run it async and poll with `read_bash`.
- Stills: `node agents/render.mjs --scene feature --stills 42,47,50` writes `out/still-feature-<t>.png`. Stills use output time. If `TIME_SCALE` is set, convert scene times to output times before choosing still times.
- Review stills before a full render. A full render is expensive; stills at every beat boundary catch most framing errors in about a minute.
- If a render is stopped mid-way, the MP4 is truncated and unusable. Delete it and re-render from scratch.
- Verify output with `ffmpeg -i <file> 2>&1 | grep Duration`, then decode with `ffmpeg -v error -i <file> -f null -`. `ffprobe` output was unreliable here.
- `timeout` is not available on macOS. Filter the noisy GL driver logs with `grep -v "GL Driver"`.
- Tile review frames with ffmpeg `xstack`. Pass inputs explicitly, because glob order scrambles frames.
- Delete scratch stills after review. Keep only the deliverable MP4 and any keyframe sheet the user asked for.

## 11. Review loop

1. Render stills at each beat start, mid-beat, and end, plus every camera hold and pan boundary.
2. Check each still for: subject in frame, caption readable, nothing floating, no sub-pixel specks, and the shot still tells the current beat.
3. Check the timeline for empty stretches longer than the caption needs. Fill them with real activity.
4. Fix in the scene file, re-render the affected stills, then re-render the full MP4 once.
5. Verify duration and decode, then present keyframes or the video for review.
6. Do not commit unless the user asks.

## Anti-patterns seen in this project

- Empty conveyor and arm pauses with no movement.
- Objects appearing mid-frame instead of arriving on a path.
- Camera moves that cut away from the subject before its result is shown.
- Literal visuals that repeat the caption (for example, a drill labelled "edit") instead of letting the image carry the metaphor.
- Captions that describe a tool or mechanism that does not exist in the real CLI.
- Rushed beats that give the viewer no time to read.
- Tiny tool or cargo geometry that flickers at sub-pixel scale.
- Partially written MP4 files left after an interrupted render.
