# Changelog

All notable changes to `keynope` are documented in this file.

This project follows semantic versioning.

## 1.8.1

Fixed:

- Participant slide tabs backed by an authored Keynope page no longer display their compressed transfer payload as literal text. Reference and cheat-sheet tabs now render as slides again.

## 1.8.0

Added:

- Editable PowerPoint (`.pptx`) export from the Mac and web editors, plus PowerPoint import that converts slides into an editable Keynope deck.
- Portable embedded C64 font instances in PowerPoint exports. Each C64 width used by a deck is embedded as its own editable face, preserving its authored height and horizontal proportions on other machines.
- Slide background media: promote an image to a slide-owned background layer while retaining its image treatment, including brightness, colour scale, Retro conversion, effects, opacity, and fit mode.

Changed:

- PowerPoint exports use the same 16:9 slide geometry as Keynope and preserve editable text boxes, shapes, images, links, notes, and basic styling wherever PresentationML supports them.
- PowerPoint text now preserves horizontal and vertical alignment within its Keynope bounding box, including justified text.

Fixed:

- PowerPoint-exported C64 text no longer relies on point-size reduction to compensate for Keynope’s independent width setting, which could make letters too short or clip them inside a text box.

## 1.7.1

Changed:

- Font-family changes now preserve approximate visual dimensions. C64, Go Mono, and Go proportional text use measured optical width normalization while retaining the authored font size.
- Font size and font width controls now use consistent grouped fields with directly associated increase and decrease buttons.

Fixed:

- Numeric inspector fields immediately reflect direct edits and relative `+` / `−` changes instead of displaying the previous value.
- Shape-label typography, paragraph spacing, opacity, width, and rotation fields no longer fall back to stale rendered-scene values after a mutation.

## 0.1.7

- Added modern mode
- Overhaul of interface

## 0.1.6

Added:

- Interactive workshop activities with on-slide launch markers, QR-code joining, browser participation, live responses, presenter-controlled reveals, and configurable timers and anonymity where appropriate.
- Polling, brainstorming and voting activities including Pulse, Storm, Sort, Dot Voting, Questions with a voting round, Quiz, and Fact or Fiction with up to five sequential questions and answer reveals.
- Group exercises including Pair Share, Playing Cards, The Race, and Prerequisites. Prerequisites combines per-item instructions and checklists with completion timing; completion-based pairing balances faster and slower participants. Assigned groups receive their own private pairing chat.
- Creative and discussion activities including Quick Draw, a layered pixel-art Introduction avatar builder, Gallery Walk, Feedback Wall, Expertise, Ball Toss, and private Impostor role assignments.
- An Onboarding lobby with a reusable deck-persisted session code, participant list, group chat, private messages, and a live Presentation view. Participants can follow slides and activities from desktop or mobile browsers, with animation scheduling matched to the native/export renderer.
- Participant tabs for URLs and reference slides, configured through Settings. Slides can also be toggled into a separately ordered Tabs section, excluded from presentation navigation, and restored to their previous position.
- TrueType text using the bundled Keynope C64 font, with independent font-size and horizontal-width controls, heading presets, slide/master typography defaults, and editable wrapping boxes. Text supports gradients, shadows, outlines, rotation, transparency, and Blocks, Braille, ASCII and Dense rendering treatments.
- Text justification within a bounding box, leaving lines shorter than 70% of the longest unadjusted line untouched.
- Colour TrueType versions of all 3,993 bundled emoji artworks, shared across the Mac app, web editor, picker and HTML exports. Emoji tint and image colour-scale controls preserve shading while applying a selected colour.
- Shift-drag rectangle selection with live selection previews, persistent element groups, group movement and alignment, and individual member editing within a group.
- Editable text inside shapes, independently styled and centred within the silhouette. Text overflow expands the right or bottom edge as needed.
- Shape connectors with straight or elbow paths, editable bends, obstacle-aware automatic routing, optional single or double arrowheads, configurable widths and colours, and slide/master colour inheritance. Connections remain attached when shapes move or resize and are preserved in decks and HTML exports.
- Optional timer broadcasting to participant browsers. During presentation with activities defined, the timer button cycles through local, broadcast and off; otherwise it remains on/off. Broadcast mode has a green button and a BROADCAST tag.

Changed:

- Reorganised editor actions into contextual ribbon tabs. Text alignment controls position text within its box; Arrange alignment controls position the element itself.
- Text-box corner dragging adjusts margins and wrapping instead of font size.
- Standard text now uses TrueType rendering. Legacy bitmap text is upgraded on load and may lay out differently; explicit custom glyph and FIGlet fonts remain supported. Literal asterisks no longer automatically apply bold formatting; use the Bold control.
- Shape resizing supports finer, partial-block dimensions.
- The Mac app reopens the most recently opened deck when it can be restored, falling back to the starter presentation.

Fixed:

- Local presentation views refresh edited slide content without requiring an app restart.
- Overflowing master content no longer creates additional presentation pages.

Removed:

- The terminal CLI and standalone presenter-helper distribution. Builds, release archives, installation instructions and Homebrew packaging now target the windowed Mac app, which retains its private Go engine. The browser editor remains available.

## 0.1.5

Added:

- Self-contained WebAssembly editor at `keynope.sh/editor`, with browser-native New, Open, Save, image import, HTML export, presentation controls, master slides, timers, draft recovery, and offline application assets. It includes identity-safe mouse and keyboard selection, dragging, resizing, double-click text editing, slide navigation, and labelled Keynope controls.
- Browser-native image importing, correctly timed animated GIF playback, timer keyboard controls, persistent draft recovery, and repeatable saves without requiring a refresh.
- Custom font libraries for the Mac and web editors. Fonts can be created in the built-in glyph editor, painted with block and partial-block characters, resized per glyph, and stored for reuse.
- Full-size glyph editing with block brushes, click-to-toggle painting, glyph-local undo and redo, clean cell spacing, and automatic persistence for imported fonts.
- JSON and FIGlet (`.flf` and `.tlf`) font import. FIGlet fonts support editable ASCII and non-emoji Unicode glyph cells.
- Portable deck-embedded fonts. Used fonts are gzip-compressed into Markdown metadata and render consistently in the terminal, Mac app, web editor, presenter, and HTML exports.
- Horizontal, vertical, and diagonal colour gradients for text and block-rendered images.
- Soft and hard coloured shadows for text and block-rendered images, including animated image frames.
- Contextual gradient and shadow controls with colour selection in the editor toolbars and element menus.

Changed:

- Unified the Mac and web colour-picking interface and renamed “Show colors” to “Pick color”.
- Contextual element toolbars now hide unrelated document controls while an element is selected.
- The Mac app and terminal New flow now share the same polished 245×56 two-slide starter presentation.

## 0.1.4

Fixed:

- Use user-selected file access for presentations in Downloads instead of requesting unrestricted Downloads-folder access.

## 0.1.3

Fixed:

- Include the Keynope app icon in release archives built with older Xcode versions.

## 0.1.2

Added:

- Mac app for Keynope
- Bunch of improvements

## 0.1.1

Fixed:

- Release binary signed with valid Apple Developer ID certificate
  This prevents having to re-authorise screen sharing after every release.

## 0.1.0

Initial keynope release
