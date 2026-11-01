# keynope-slides

Authoring guidance for building and editing Keynope decks through its MCP server, based on Josh Gonggryp's contribution. Since 1.8.9, this guidance is embedded in the app: connected agents can call `deck_authoring_guide` or read `keynope://authoring/skill` without installing this folder. Native tools replace the original Python helpers and return compact edit receipts.

`SKILL.md` can also be installed independently in clients that support skills. The native MCP tools are client-independent.

## Contents

| File | What it does | Needs |
|---|---|---|
| `SKILL.md` | The instructions the agent loads | |
| `reference.md` | Less common recipes and details, read only when `SKILL.md` points there | |

## Install

Copy the folder into your personal skills, or into a deck project's `.claude/skills/`:

```sh
cp -R skills/keynope-slides ~/.claude/skills/
```

## Connect Keynope

In Keynope, open **Automation (MCP)**, tick **Enable MCP access** and click **Copy Claude Command**. Run what it copies; with the default port on 1.8.8 that's:

```sh
claude mcp add --transport http keynope 'http://127.0.0.1:50987/mcp'
```

This server runs while Keynope is open and keeps its automation deck between agent sessions.

To use Keynope without opening the app, let Claude Code start a headless server instead. In the Mac App Store build the workspace must be the folder shown in the Automation (MCP) window; other folders hang, because the folder-access dialog can't appear without a window.

```sh
claude mcp add keynope -- /Applications/Keynope.app/Contents/MacOS/Keynope --mcp --workspace "$HOME/Library/Containers/sh.keynope.app/Data/Library/Application Support/Keynope/Automation"
```

Both ways run the same server with the same tools. Register only one of them under the name `keynope`.

## Fewer permission prompts

Claude Code asks before it uses each new Keynope tool. It also can't copy finished decks out of the automation folder, because that folder is outside your project. To allow both, add this to your project's `.claude/settings.json` (or `.claude/settings.local.json` to keep it to yourself):

```json
{
  "permissions": {
    "allow": ["mcp__keynope"],
    "additionalDirectories": ["~/Library/Containers/sh.keynope.app/Data/Library/Application Support/Keynope/Automation"]
  }
}
```

If the Automation (MCP) window shows a different folder, use that one.

## Native tools

Use `deck_objects` for discovery, `text_measure` and `table_measure` for sizing,
`slide_measure` for layout checks, `diagram_layers` for aligned artwork,
`image_pixelart` for block-text conversion, `animation_create` for fireworks,
and `qr_code_create` for embedded vector QR codes. Assign layouts with the
native `set-layout` command. No Python, Pillow or external QR service is needed.

Go regression tests cover these tools, compact receipts, stale-edit protection,
history, embedding and MCP protocol behaviour. Browser acceptance tests live
under `internal/app/testdata/` and exercise actual rendered results.

If `slide_screenshot` fails, preserve its diagnostic and review an HTML export;
do not claim screenshots were verified. The app includes a separately signed,
sandbox-inheriting renderer for native captures.
