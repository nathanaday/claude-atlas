# Visual Customization

Apply during scaffold. This makes the file explorer color-coded by folder type and adds custom callout styles.

---

## The vault's snippet

Every claude-atlas vault carries `.obsidian/snippets/claude-atlas.css`, enabled in
`.obsidian/appearance.json`, and `claude-atlas upgrade` adds both to older vaults.
It gives one color to each kind of place: the wiki and each of its folders,
`inbox/`, and `ideas/`, so the kinds of place show in the file explorer. It also
defines the four custom callouts below. Do not create a second snippet for the
same folders; to change a color, edit the variables at the top of that file.

If the file explorer shows no colors, tell the user: Settings > Appearance > CSS
snippets, toggle `claude-atlas` on, or reload Obsidian (Cmd+R) after an upgrade.

## Graph View Groups

Guide the user to set these in Graph View settings (click the settings icon in the graph view):

| Query | Color |
|-------|-------|
| `path:wiki/domains` | Blue (`#4fc1ff`) |
| `path:wiki/entities` | Purple (`#c586c0`) |
| `path:wiki/concepts` | Yellow (`#dcdcaa`) |
| `path:wiki/sources` | Orange (`#ce9178`) |
| `path:.raw` | Gray (dimmed) |

---

## Custom Callouts

This vault defines **four custom callout types** beyond Obsidian's built-in set (`note`, `tip`, `warning`, `info`, `todo`, `success`, `question`, `failure`, `danger`, `bug`, `example`, `quote`). They render correctly **only when `vault-colors.css` is enabled**. Without the snippet, they fall back to default callout styling (still readable, just plain).

| Custom callout | Color | Icon | Use for |
|---|---|---|---|
| `contradiction` | reddish-brown (rgb 209,105,105) | `lucide-alert-triangle` | New source conflicts with existing claim |
| `gap` | beige (rgb 220,220,170) | `lucide-help-circle` | Topic has no source yet |
| `key-insight` | bright blue (rgb 79,193,255) | `lucide-lightbulb` | Important takeaway worth highlighting |
| `stale` | gray (rgb 128,128,128) | `lucide-clock` | Claim may be outdated, source older than threshold |

### Usage

Use these in wiki pages to flag important states:

```markdown
> [!contradiction] Title
> [[Page A]] claims X. [[Page B]] says Y. Needs resolution.

> [!gap] Title
> This topic has no source yet. Consider finding one.

> [!key-insight] Title
> The most important takeaway from this section.

> [!stale] Title
> This claim may be outdated. Source was from 2022.
```

### Why custom callouts (vs built-in)

The four custom types map to wiki-specific concepts that don't fit cleanly into Obsidian's default set:

- `contradiction` is more specific than `warning`: it signals a **resolvable conflict** between two wiki pages, not a generic warning.
- `gap` is more specific than `question`: it signals a **missing source**, an actionable improvement.
- `key-insight` is more specific than `tip`: it marks **the** most important takeaway from a section, used sparingly.
- `stale` has no built-in equivalent: it signals time-based decay of a claim.

If you don't want custom callouts, replace them with built-ins:
- `[!contradiction]` → `[!warning] Contradiction`
- `[!gap]` → `[!question] Gap`
- `[!key-insight]` → `[!tip] Key insight`
- `[!stale]` → `[!warning] Stale`

---

## Theme compatibility

The snippet uses standard CSS variables and does not require a community theme.
If the user chooses a theme, verify contrast and selector behavior in the
installed Obsidian version before recommending it for accessibility-sensitive
work.
