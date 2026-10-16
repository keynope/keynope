# Format and export details

The editable source is a portable Markdown deck. Metadata stores layout,
styling, embedded media, themes, masters, tables, charts, builds and activities.
This document describes technical boundaries; the [README](../README.md)
introduces the user-facing capabilities.

## Document behaviour

Templates create an unsaved copy of their slides and masters. Theme and element
styles travel in the deck rather than depending on a locally installed template.
Elements can follow theme roles or retain explicit formatting. Detaching a theme
keeps its resolved appearance and editable master relationships.

Text is semantic and uses the bundled TrueType fonts. Older bitmap, custom-glyph
and FIGlet text is upgraded on load while retaining its content and placement;
saving writes the supported TrueType representation. PowerPoint conversion uses
the same editor migration for older shapes, labels and attached connectors.

Master definitions are metadata, not additional presentation slides. Named object
labels are separate from the stable identities used by builds and connections.
Internal slide links follow identifiable destinations; cross-deck paste removes
unresolvable internal links with a warning rather than targeting unrelated pages.

Physical page dimensions are separate from the Markdown text grid and apply to
every slide and master. Supported PowerPoint conversions retain physical size.

## Validation and limits

Loading and saving share validation boundaries. Rejected candidates do not
replace the current document; rejected edits preserve undo history. Fenced code
examples remain literal and are not interpreted as document metadata.

| Data | Limit |
| --- | --- |
| Metadata comments | 100,000 markers and 256 MiB of comment bytes |
| Comments plus known gzip expansion | 512 MiB |
| Embedded media archive | 100 MiB decompressed JSON |
| Decoded media | 64 million pixels per asset across compact image, original and frames |
| Animation | 2,048 frames per representation |
| Combined table/chart/build registry | 10,000 entries, 1 million stored table cells, 2 million chart values, 16 MiB stored text or canonical JSON metadata |
| Master or participant-tab archive | 16 MiB JSON each |
| Document extensions | 1 MiB JSON |
| Activity definition, completed result or background metadata | 1 MiB JSON each; result limit applies after decompression |
| Markdown grid | 4,096 cells per axis and 1,048,576 cells total |

These are not compressed Markdown file-size limits. Individual object and
conversion limits also apply. Media pixel limits are per asset, not per deck.

Known metadata rejects unsupported versions, ambiguous duplicates, malformed
encoding and oversized payloads rather than silently discarding them. Placement
fields require valid finite numbers and unambiguous URL/page links, including
in masters, image queries and shape labels. Compatible future fields survive
ordinary editing and saving. Embedded images are checked against their declared
type, dimensions and complete pixels before admission.

## Audience data

Editable decks may contain speaker notes, participant names and completed
activity responses. Onboarding session codes persist; per-activity runtime codes
do not. Treat the source deck as workshop data.

Audience slide projections and static exports omit private result archives and
speaker notes. Participant Markdown also excludes hidden objects, unused
media/data, stale text-formatting archives and private extension metadata.
Public render pages omit editing-only capabilities, continuation data and
attachment hit-test records. These filters do not remove personal information
deliberately authored as visible slide content.

## Export comparison

| Content | Standalone HTML | PDF | Editable PowerPoint |
| --- | --- | --- | --- |
| TrueType text | Shared layout and effects | Selectable where quality export permits | Text and word styles; some effects and width scaling approximated |
| Tables | Shared cells, tracks and borders | Quality text/vector or page-image fallback | Native tables; merged-border display can vary by reader |
| Charts | Vector marks and accessible data | Quality text/vector or page-image fallback | Native charts with embedded workbook data; some styles approximated |
| Shapes and connectors | Shared geometry and routing | Vector or page-image fallback | Supported editable presets/custom paths and attached connectors |
| Images and GIFs | Shared crop, mask and treatment | Static image output | Embedded media and supported adjustments; viewer-dependent GIF playback |
| Builds and transitions | Interactive playback | Static pages | Supported basic timing only |

### PDF

The Mac exporter offers Quality (selectable text and vectors) and Exact
appearance (image pages). Quality output uses page-image fallback when it cannot
safely reproduce a page. Sampled images remain independent of selectable text;
native capture checks their drawing operations before accepting quality output.
PDF is static and captures animations at their first frame.

### PowerPoint

Conversion does not require PowerPoint to be installed. Editable export is not
a whole-slide exact-appearance capture: check conversion notes and inspect the
result in the intended viewer.

- Supported text, tables, charts and shapes remain editable. Charts include
  workbook data. Basic builds and transitions are mapped; arbitrary Office
  animation trees and animated slide effects are not recreated.
- Authored palettes and heading/body font faces export into the PowerPoint
  theme. Imported colours and Latin major/minor font references resolve through
  slide/layout/master relationships and local overrides. Supported primary-theme
  references remain live; explicit formatting and independently themed masters
  remain concrete. Extra roles and typography settings may use object formatting.
- Theme-linked word/chart colours may export as current RGB values; some
  table/chart effects are omitted. Text and shape-label two-stop gradients remain
  editable, alongside supported outlines, shadows and opacity. Viewers can
  distribute gradients across runs and paragraphs differently.
- Image shadows remain editable. The Mac and browser editors capture image
  gradients, pixel-following outlines and rendered Retro artwork as pictures,
  retaining editable placement, rotation and opacity rather than substituting
  the unadjusted source. Retro artwork is not editable PowerPoint text; ordinary
  C64 TrueType text remains text.
- Supported foreground/background animations export as rendered GIFs. Palette,
  pixel-alpha and 10 ms timing limits are reported. Failed animation capture can
  fall back to the first rendered frame with a warning. Routes without renderer
  capture, unsupported sampled objects and over-budget captures may report
  unavailable artwork. Use HTML or exact-appearance PDF when appropriate.
- Flowchart shapes use equivalent adjustable presets or native vector paths.
  Supported custom paths retain catalogue parameters on unchanged re-import;
  arbitrary externally edited freeform paths produce an approximation warning.
- Attached straight and orthogonal connectors retain native endpoint references,
  bends, colour, transparency and arrows where supported. Office arrow dimensions
  and foreign connection sites may be approximated; unsupported or ambiguous
  attachments use diagnosed artwork fallbacks.

Conversion reports include slide/object locations, fallback and suggested next
steps. They stay in editor memory, not saved Markdown or audience output.

Image-conversion work and package output size are bounded. Exceeding a limit
fails without returning a partial package. Mac export provides cancellable
preparation/writing progress, stages output before replacement and refuses to
overwrite a destination changed by another writer during export.
