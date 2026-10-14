# Glint design direction

Features come from Screendrop. Interaction patterns follow CleanShot X, the reference for
screenshot UX. The look is native Windows 11, so Glint is neither app's clone.

## Interaction patterns (CleanShot)

- **Quick Access overlay.** Captures appear as rounded thumbnail cards stacked in the bottom-left
  corner (configurable side). Actions show on hover: two large pill buttons in the middle
  (Copy, Save), small round icon buttons in the corners (close, edit, pin, more). Drag the card
  itself to drop the file into another app. Swipe or drag a card off-screen to dismiss.
- **Capture overlay.** Full-screen frozen dim with crosshair, live size label and magnifier.
  Window hover highlights the window. Esc cancels, Space toggles window mode.
- **Annotation editor.** One window. A single top toolbar with the tools, a contextual style bar
  for the selected tool (color, width, fill, font), the canvas in the middle, and a right sidebar
  for background, padding, frame and effects. Bottom bar with zoom and the Copy / Save / Done actions.
- **Recording.** Minimal floating control bar (timer, pause, restart, stop, delete) near the
  bottom of the recorded display; area recordings keep a dashed outline around the region.
- **Library.** Capture-history window: sidebar filters, thumbnail grid with hover actions, inspector.
- **Studio.** Large preview, timeline across the bottom, settings sidebar on the right.

## Look (Windows 11)

- Mica or acrylic-like dark/light surfaces, 8 px corner radius on windows and cards, 4 px on controls.
- Segoe UI Variable (fallback Segoe UI), 13-14 px body, semibold headings.
- Accent from the Windows accent color; neutral grays otherwise.
- Subtle 1 px borders and soft shadows instead of heavy chrome. Fluent-style icons (outlined, 1.5 px stroke).
- Follow the system light/dark theme.

## Rules

- No Screendrop or CleanShot names, logos, icons, illustrations, wallpapers or marketing strings.
- Glint's own app icon and accent identity.
