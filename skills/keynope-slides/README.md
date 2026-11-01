# keynope-slides

An agent skill for building and editing Keynope decks through Keynope's MCP server. It gives the agent tested command JSON, query keys, effect and font names, layout workarounds and a low-token workflow, so it doesn't spend hundreds of thousands of tokens on `deck_schema`, `slide_inspect` and trial and error. It doesn't change Keynope itself.

It's written for [Claude Code](https://claude.com/claude-code). `SKILL.md` follows the common skill format, so other clients that read skills may work too, but they aren't tested.

## Contents

| File | What it does | Needs |
|---|---|---|
| `SKILL.md` | The instructions the agent loads | |
| `reference.md` | Less common recipes and details, read only when `SKILL.md` points there | |
| `scripts/list_ids.py` | Element IDs, positions and table data per slide from a saved deck | Python 3 |
| `scripts/set_layout.py` | Sets slide layouts in a saved deck, so master elements show (no MCP command does this) | Python 3 |
| `scripts/fit.py` | Line counts and heights for text and table rows before placing them | Python 3 |
| `scripts/scene_bounds.py` | Real rendered bounds per slide from an HTML export | Python 3 |
| `scripts/ascii_layers.py` | Splits an ASCII diagram into aligned layers for builds | Python 3 |
| `scripts/pixelart.py` | Turns a logo image into a coloured block-character text element | Pillow |
| `scripts/fireworks_gif.py` | Renders a looping fireworks GIF overlay | Pillow |
| `scripts/selftest.py` | Checks this skill against the installed Keynope | Python 3, Keynope |

## Install

Copy the folder into your personal skills, or into a deck project's `.claude/skills/`:

```sh
cp -R skills/keynope-slides ~/.claude/skills/
```

## Connect Keynope (1.8.6 or later)

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

## Check it against your Keynope

```sh
python3 scripts/selftest.py --url http://127.0.0.1:50987/mcp   # through Keynope's in-app server
python3 scripts/selftest.py                                     # through a headless server it starts itself
```

The self-test builds a throwaway five-slide deck in the automation workspace and checks the commands, workarounds and scripts the skill relies on. It deletes its files afterwards (`--keep` leaves them) and never calls `slide_screenshot`.
- With `--url`, Keynope must be open with **Enable MCP access** ticked. The test only runs while the in-app automation deck is a blank Untitled deck, and leaves it blank again. `--discard` lets it replace a deck you no longer need.
- Run it with a Python that has Pillow to check the two image scripts as well.
- Pass `--workspace` and `--keynope` if yours are elsewhere.

Last run: Keynope 1.8.8 (Mac App Store build), macOS 15.7.7 on Apple silicon. All checks passed, both through the in-app server and through a headless one.

## Known limits

- Tested only with the Mac App Store build. There, `slide_screenshot` fails ("render slide: signal: trace/BPT trap") and macOS shows a crash dialog, so the skill previews the HTML export instead.
- The command details come from Keynope's MCP behaviour, not from a documented API. When Keynope changes, run the self-test before relying on the skill.
