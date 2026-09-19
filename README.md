# Keynope

Keynope is a retro presentation editor for macOS and the web. This repository builds the windowed Mac app and the self-contained browser editor. The terminal CLI has been retired.

![Keynope main presentation screen](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn1-keynope-main-screen.jpg)

## App Store

You can download and install Keynope from the Apple [App Store](https://apps.apple.com/us/app/keynope/id6793679625)
Don't forget to give it a kick ass review.

## Install with Homebrew

```sh
brew tap keynope/keynope
brew install --cask keynope
```

## Community

Have an idea, suggestion, feedback, or a memorable Keynope anecdote? [Join the conversation in Keynope Discussions](https://github.com/keynope/keynope/discussions). Early experiments, presentation stories, feature concepts, and constructive criticism are all welcome.

Want to contribute code or report a bug? Read the [contribution guide](CONTRIBUTION.md).

## Build requirements

- Go
- macOS 14 or later with `swiftc` available through Xcode Command Line Tools

## Build

```sh
make build
```

This creates `bin/Keynope.app`, including its private Go engine. There is no standalone terminal executable or presenter helper to install.

## Run

```sh
open bin/Keynope.app
```

Use File → Open to select a Markdown deck, or File → New for an unsaved starter deck. Save and Export to HTML use native file dialogs. Present locally or on an external screen using the presentation controls.

## Text

Text uses the bundled Keynope C64 TrueType face by default. The T button inserts “Text”; H1, H2 and T select heading, subtitle and body sizes. Size and horizontal width are independently adjustable. Dragging the text-box corners changes its available space and wrapping, not the font size.

Text is always stored as semantic text and rendered through the shared TrueType layout engine. Apply the Retro treatment for the bundled C64 face or Modern for the bundled professional fonts. Gradients, outlines, shadows, rotation and transparency work in both treatments.

Older bitmap, custom-glyph and FIGlet text is upgraded on load while retaining its content and placement. Saving writes the supported TrueType representation; retired font artwork and sampled-text settings are not retained.

## Visuals and Effects

Slides can combine terminal-art images with configurable backgrounds and animations.

![A Keynope presentation using an animated data-storm background](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn7-backgrounds-and-animations.jpg)

Effects adapt to the slide palette so they remain part of the composition on light or dark backgrounds.

![A Keynope effect adapting to a light slide background](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn3-effects-adapt-to-background.jpg)

## Engagement Activities

The Mac and web editors add an activity slide from the permanent Activities icon in the bottom toolbar:

- **Pulse** records a quick 0–5 confidence check or a custom poll.
- **Storm** collects short ideas and reveals them together.
- **Sort** lets the facilitator drag cards into named destinations.

Each activity is inserted after the current slide with the built-in Activity master, an eight-character room code, and a centered ASCII QR code for its `keynope.sh/join/<code>` URL. Keynope monitors the short-lived room automatically during presentation while leaving the activity slide visible. Participants choose a remembered display name and respond from their own device. The presenter receives aggregate votes, ideas, card placements, and the names of respondents live. Open the controls and results from the Activities icon or with `G` in an HTML presentation.

Every activity follows the same `OPEN → LOCKED → REVEAL → DISCUSS` flow; `Enter` advances, `R` resets the current responses, and `Esc` or `Q` closes it. Activity definitions and codes travel with the Markdown deck as `keynope-engagement` metadata. Rooms expire after 24 hours of inactivity, and participant responses are never written into the deck.

### Colour emoji fonts

All 3,993 bundled emoji artworks are also available as Keynope Emoji colour
TrueType faces (COLRv0, WOFF2-compressed). Mac, web, the emoji picker and HTML
exports use the same artwork and metrics, including complete flag, skin-tone
and joined emoji sequences. Emoji retain their colours and square proportions
alongside the C64 text face. Text size, explicit width, glyph treatments,
gradients, outlines, shadows and see-through apply through the shared renderer.
Select an emoji and use **Style → Emoji tint** for a single-colour scale that
preserves shading. White gives grayscale; disabling it restores the original
colours. For mixed text and emojis, only the emojis are tinted.
Only faces used in exported text are included in the export; Markdown keeps the
Unicode text and styling, not a second copy of this built-in font collection.

Normal builds consume the generated `assets/emoji/keynope-emoji-fonts.zip`;
normal builds do not require Python or FontTools. To regenerate from the
canonical block artwork, install `fonttools[woff]` in a Python environment and
run `python tools/build_emoji_fonts.py`. Individual uncompressed `.ttf` files
are additionally written to `output/emoji-fonts/`. These are per-artwork faces
used by Keynope's sequence mapping, not a single installable system emoji font.
The derivatives retain the bundled OFL and attribution notices.

## Grouping elements

Hold Shift and drag on the canvas to select several elements with a rectangle. Yellow highlights preview the items that will be added to your selection on release; Escape cancels. Shift-click still toggles individual items. Then right-click → Group (also available under Arrange). A group has one outer bounding box; dragging, arrow-key movement and Arrange alignment preserve the spacing between its members. Groups are saved in the deck's Markdown metadata and support undo/redo.

Double-click a group to expose its individual elements. Select a member to edit, resize or adjust it, then press Escape or choose Done editing group to return. Right-click → Ungroup separates the members without changing their appearance or positions. Selecting a group together with other items and choosing Group combines them into one flat group.

For mixed selections, text tools affect only text, and Colour affects text and shapes. Image adjustments, links and other single-item tools are available when editing an individual member.

## Text inside shapes

Double-click a shape to add or edit its text. Only the overflowing axis grows: width extends to the right, height extends downward, keeping the top-left corner fixed. Text is re-centred horizontally and vertically inside the expanded silhouette. Shape text belongs to the shape, so moving, grouping, copying and deleting it keep them together.

Select the shape to adjust its text size, width and bold weight in the Shape tab. The Style tab has separate text colour, gradient, shadow, outline, rendering and transparency controls, independent of the shape's fill. Clearing the text keeps the shape.

## Connecting shapes

Choose **Connect shapes** in Insert (or the selected shape's Shape tab).
Drag between the yellow connection dots, or click a dot on each shape.
Each shape has top, bottom, left and right centre ports on its visible silhouette.
Press Escape or click empty canvas to cancel. New connectors use elbow paths,
preferring routes around visible shape pixels; unavoidable crossings receive a
heavy routing penalty. Lines stay attached when shapes move or resize.
Connect shapes shows path, arrowhead, width and colour controls before drawing;
selecting a line shows the same controls plus Delete. There is no line right-click menu.
Line width defaults to 1 (the minimum), arrow width to 6, with arrowheads off.
Unset colour inherits the slide/master colour; Inherit color clears an override.
Horizontal strokes use half the canvas-height units for equal visual thickness.
Lines and arrowheads are rendered above shapes from continuous semi-blocks, including
the styled drag preview—there is no yellow line overlay. Double-click an elbow line
to expose segment handles: drag horizontal segments up/down or vertical segments
left/right. Manual bends are saved; **Auto-route** restores automatic routing.
Connections are saved in the deck and render in presentations and HTML exports.

## Tab-only slides

Use **Tab** in the bottom toolbar to turn the current slide into a participant tab.
It moves into the separate **Tabs** section beneath Slides; drag those entries to
change their tab order. Tab-only slides stay editable but are skipped during
presentation navigation, including HTML exports. Participants see them alongside
the existing tabs in the activity lobby. Toggle **Tab** off to restore the slide
to its previous position. Tab status and order are saved in the deck's Markdown.

## Master Decks

Press `M` while the editor chrome is visible to open **Master View**. Every deck can contain a Base Master and multiple named layouts:

- The Base Master supplies deck-wide graphics, backgrounds, effects, and styles.
- Layouts add fixed graphics and editable Title, Subtitle, Body, Code, or Image placeholders.
- New decks include a dynamic page number in the Base Master's bottom-right corner. Press `#` either in Master View or while editing a master to show or hide it on the Base, or cycle inherit/show/hide on a layout. The number itself can be selected, moved, resized, colored, styled, and outlined while editing the master.
- Press `v` in Master View or while editing a master to open Visual Properties. It controls foreground, slide background, header color, background pattern, and effect. Layout properties can inherit from Base, explicitly choose a value, or explicitly choose None.
- Select an element in Master View and press `p` to assign or remove its placeholder role.
- Create, clone, rename, reorder, and delete layouts from the Master View navigator.
- Master elements render behind slide-owned content and cannot be selected from an ordinary slide.

Press `n` to create a slide. Keynope opens a visual layout chooser and preselects the current slide's layout. Press `L` to apply another layout to an existing slide. Matching placeholder content is rebound by slot and role; content that has no matching placeholder is preserved as an ordinary local element.

![Choosing a master-deck layout for a new Keynope slide](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn2-build-master-decks.jpg)

Press `#` in the normal slide view to cycle that slide's page-number policy through inherit, show, and hide. Page numbers resolve from the actual slide index in the presenter, thumbnails, and HTML export; no literal number is stored in slide content.

Press `v` in the normal slide view to edit the same five visual properties for that slide. Mastered slides can inherit each property independently, choose an explicit value, or choose None. Changes remain local to that slide. Selecting **Close menu** commits immediately; pressing `Esc` after making changes asks `Save visual changes? [Y/n]`, with Yes as the default.

![Customising the visual properties of an individual Keynope slide](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn4-customise-every-slide.jpg)

Master definitions are embedded as versioned metadata in the deck's `.md` file. They do not count as slides and are not included directly in presentation or HTML export. Decks created by older Keynope versions remain valid and unmastered until a master or layout is used.

## Install

```sh
make install
```

By default this installs `Keynope.app` in `/Applications`. To install for your user only:

```sh
make install APPDIR="$HOME/Applications"
```

## Project Layout

```text
main.go                         shared presentation engine and windowed editor
fonts.go                        Glyph/font data
masters.go                      master-deck model, persistence, and resolution
master_ui.go                    master preview and placeholder helpers
presenter/KeynopePresenter.swift native macOS application
presenter/ScreenShare.swift       native app, window, and screen capture
presenter/EmbeddedIcon.swift      embedded menu bar icon data
app/Info.plist                  application bundle identity and privacy metadata
assets/KeynopeMenuTemplate.png    source icon asset
Makefile                          build, sign, test, install, clean
```

Generated files live in `bin/` and are not committed.
