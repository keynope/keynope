# Changelog

All notable changes to `keynope` are documented in this file.

This project follows semantic versioning.

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
