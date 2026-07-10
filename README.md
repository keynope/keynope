# Keynope

Keynope is a terminal-native presentation tool. This repository is the source distribution: it builds the Go CLI and the native macOS presenter helper from source.

![Keynope main presentation screen](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn1-keynope-main-screen.jpg)

## Install with Homebrew

```sh
brew tap keynope/keynope
brew install keynope
```

## Build requirements

- Go
- macOS 14 or later with `swiftc` available through Xcode Command Line Tools

## Build

```sh
make build
```

This creates:

```text
bin/keynope
bin/KeynopePresenter.app
```

The Go CLI launches `KeynopePresenter.app` automatically when the app bundle is next to the CLI. If the helper is absent or unavailable, Keynope still runs in presentation mode locally and simply skips second-screen broadcast.

The helper is an `LSUIElement` app: it has a stable `sh.keynope.presenter` identity and a menu bar icon, but no Dock icon or ordinary application window. `make build` ad-hoc signs the complete bundle. Screen Recording permission therefore belongs to Keynope Presenter instead of the terminal that launched it. An ad-hoc identity changes when the helper is rebuilt, so macOS may ask for permission again after an upgrade.

## Run

```sh
bin/keynope deck.md
```

By default Keynope starts presentation mode: the terminal UI renders at the deck's authored size. When the native presenter helper is available, it also opens a second-screen surface. That surface shows TV snow while you are editing, and switches to the live slide only while `p` presentation mode is active. If no external display is connected, the helper stays in the menu bar and does not cover the terminal. It will automatically open on an external display if one is connected later.

The presenter menu bar's **Share** submenu can place another application, individual window, or entire screen over the live deck. Shared content is aspect-fitted with a margin on every edge so the current slide remains visible around it. **Share → Nothing** stops capture. Screen sharing is video-only and macOS asks for Screen Recording permission the first time it is used; if permission is denied, the same menu links to the relevant System Settings page. Full-screen sharing excludes Keynope Presenter itself to avoid recursive capture.

Press `2` to open speaker notes while presenting. Animated GIFs and WebP images play directly inside the terminal-rendered slide.

![Speaker notes and an animated GIF in a Keynope slide](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn6-speaker-notes-and-animated-gifs.jpg)

Press `0` to start the test-card countdown timer before a presentation.

![Keynope test-card countdown timer](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn8-testcard-countdown-timer.jpg)

Create a new starter deck in the current directory:

```sh
bin/keynope
```

This opens an ASCII startup menu. Choose **Open** to select an existing Markdown deck with the system file picker, or **New** to name a new `.md` deck. If the new deck path already exists, Keynope asks before overwriting it.

Classic terminal-only mode:

```sh
bin/keynope --classic deck.md
```

Export HTML:

```sh
bin/keynope --export deck.md
```

Press `?` at any time to open the shortcut reference.

![Keynope main keyboard shortcuts](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn5-shortcuts.jpg)

## Visuals and Effects

Slides can combine terminal-art images with configurable backgrounds and animations.

![A Keynope presentation using an animated data-storm background](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn7-backgrounds-and-animations.jpg)

Effects adapt to the slide palette so they remain part of the composition on light or dark backgrounds.

![A Keynope effect adapting to a light slide background](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn3-effects-adapt-to-background.jpg)

## Master Decks

Press `M` while the editor chrome is visible to open **Master View**. Every deck can contain a Base Master and multiple named layouts:

- The Base Master supplies deck-wide graphics, backgrounds, effects, and styles.
- Layouts add fixed graphics and editable Title, Subtitle, Body, Code, or Image placeholders.
- New decks include a dynamic page number in the Base Master's bottom-right corner. Press `#` either in Master View or while editing a master to show or hide it on the Base, or cycle inherit/show/hide on a layout. The number itself can be selected, moved, resized, colored, styled, and outlined while editing the master.
- Press `v` in Master View or while editing a master to open Visual Properties. It controls foreground, terminal background, header color, background pattern, and effect. Layout properties can inherit from Base, explicitly choose a value, or explicitly choose None.
- Select an element in Master View and press `p` to assign or remove its placeholder role.
- Create, clone, rename, reorder, and delete layouts from the Master View navigator.
- Master elements render behind slide-owned content and cannot be selected from an ordinary slide.

Press `n` to create a slide. Keynope opens a visual layout chooser and preselects the current slide's layout. Press `L` to apply another layout to an existing slide. Matching placeholder content is rebound by slot and role; content that has no matching placeholder is preserved as an ordinary local element.

![Choosing a master-deck layout for a new Keynope slide](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn2-build-master-decks.jpg)

Press `#` in the normal slide view to cycle that slide's page-number policy through inherit, show, and hide. Page numbers resolve from the actual slide index in the terminal, presenter, thumbnails, and HTML export; no literal number is stored in slide content.

Press `v` in the normal slide view to edit the same five visual properties for that slide. Mastered slides can inherit each property independently, choose an explicit value, or choose None. Changes remain local to that slide. Selecting **Close menu** commits immediately; pressing `Esc` after making changes asks `Save visual changes? [Y/n]`, with Yes as the default.

![Customising the visual properties of an individual Keynope slide](https://raw.githubusercontent.com/keynope/keynope/main/screenshots/kn4-customise-every-slide.jpg)

Master definitions are embedded as versioned metadata in the deck's `.md` file. They do not count as slides and are not included directly in presentation or HTML export. Decks created by older Keynope versions remain valid and unmastered until a master or layout is used.

## Install

```sh
make install
```

By default this installs the private CLI and app bundle under `/usr/local/libexec/keynope`, with `/usr/local/bin/keynope` pointing to the private CLI. This is also the intended Homebrew formula layout. Override the target with:

```sh
make install PREFIX="$HOME/.local"
```

## Project Layout

```text
main.go                         Go CLI and terminal renderer
fonts.go                        Glyph/font data
masters.go                      master-deck model, persistence, and resolution
master_ui.go                    master view, layout and placeholder pickers
presenter/KeynopePresenter.swift macOS presenter helper
presenter/ScreenShare.swift       native app, window, and screen capture
presenter/EmbeddedIcon.swift      embedded menu bar icon data
presenter/Info.plist              helper bundle identity and privacy metadata
assets/KeynopeMenuTemplate.png    source icon asset
Makefile                          build, sign, test, install, clean
```

Generated files live in `bin/` and are not committed.
