---
name: ui-screenshot-documentation
description: "Use when capturing, retaking, cropping, or documenting screenshots of any OpenDepot UI; updating documentation with UI images; or reviewing UI screenshots for focus, accuracy, and readability. Triggers: 'take a screenshot', 'capture the UI', 'retake this screenshot', 'document this screen', 'update the docs screenshots', 'replace a UI image'."
argument-hint: "UI screen or feature to capture and document"
---

# UI Screenshot Documentation

Use this skill for UI screenshot and documentation work anywhere in OpenDepot. It is not tied to a particular feature, route, or service. Discover the relevant screen, documentation location, and existing asset conventions for each task.

## Before capturing

- Identify the specific UI detail each requested image should explain. Review nearby documentation and existing screenshots so the capture fits the guide and avoids unnecessary repetition.
- For every user-visible feature or workflow described in prose that has functional UI controls, include a nearby screenshot showing the relevant control and, where applicable, a representative selected or configured result. When prose lists value sources, modes, or other alternatives, make sure the screenshot shows the corresponding UI choices; split it into focused captures if needed for legibility. Do not leave actionable UI behavior as prose-only instructions when the interface can demonstrate it. Not every factual sentence needs a screenshot, and details with no meaningful visual representation do not need one.
- Reuse the user's already-authenticated shared browser page when available. Follow the user's specified URL and session requirements. Never assume `localhost:3000` is the correct target or switch to a different host when the user has specified one.
- Preserve the current application state. Prefer existing data. If temporary UI state is necessary, change only what the capture requires, record what was changed, and restore it afterward without reverting unrelated user changes.
- Use the real application UI as the source of truth. Expand previews, open panels, or reveal controls through the UI when the requested detail depends on them.

## Capture and composition

- Make each screenshot about one reader-facing point. Focus on the relevant control, field, preview, or message; do not repeat most of an earlier screenshot to show a minor difference.
- Keep the subject complete and readable at its final documentation size. Include enough surrounding UI to orient the reader, with natural breathing room around the subject. Do not clip labels, code, validation messages, or relevant controls.
- Use the browser's actual zoom and scroll position to compose the capture. If the subject needs more space, zoom out and recapture rather than manufacturing margins.
- Use only genuine pixels from the application. Cropping a screenshot is acceptable; adding borders, blank canvases, artificial padding, overlays, retouched UI, or composites is not. Do not stretch or redraw interface content to improve the framing.
- Save the screenshot to the appropriate repository documentation asset directory, following nearby filename conventions. A browser tool's attached preview alone is not a saved documentation asset.
- Inspect the saved image itself after capture. Confirm that it shows the intended state, is legible, has natural spacing, and contains no accidental clipping, scrollbars, overlays, or unrelated repeated content. If it is wrong, recapture from the browser instead of trying to repair it with invented pixels.
- Prefer capturing a stable UI element directly when supported. If the capture is clipped by a scrollable ancestor, adjust the real UI scroll position and capture again. Avoid repeated speculative viewport or coordinate changes; verify each capture before proceeding.

## Documentation

- Update the relevant guide or page and use a relative image link that resolves from that document. Add concise alt text describing the visible content and its purpose.
- Describe the UI directly and in present tense. Avoid wording such as “we now” or “this is new” unless the user specifically requests release or change-history language.
- Keep the documentation focused on the feature the image shows; do not add unrelated product claims or duplicate surrounding guidance.

## Verify and restore

- Verify every saved image visually and confirm the referenced path exists.
- Compare the prose against the screenshots before finishing. For each described feature or list of options with corresponding UI controls, confirm that a nearby image shows those controls and, where useful, a representative configured result; a general overview image alone is not sufficient.
- Run the smallest relevant documentation check, such as the configured documentation-site build, and run `git diff --check` when editing repository files.
- Restore browser dialogs, scroll position, zoom, and any temporary application state when possible. If part of the original state cannot be restored, report exactly what remains changed.
- Do not commit changes unless asked.

When reporting completion, summarize the documentation and image changes, the validation performed, and any browser state that could not be restored.
