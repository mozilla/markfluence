---
title: markfluence table cell background probe
space: ~60c36d0718e9f60071326951
parent: null
page_id: 2942664752
page_width: max
---

# markfluence table cell background probe

Published by markfluence from `table-cell-bg-probe.md` to verify that a
`<!-- bg:COLOR -->` cell marker reaches ADF as a cell `background`. Each cell below
is labeled with the swatch name that produced it; compare against the editor's
picker on "Confluence tables test".

## All 21 swatches

| Light | Medium | Bold |
| --- | --- | --- |
| <!-- bg:white --> white | <!-- bg:light-grey --> light-grey | <!-- bg:grey --> grey |
| <!-- bg:light-blue --> light-blue | <!-- bg:blue --> blue | <!-- bg:bold-blue --> bold-blue |
| <!-- bg:light-teal --> light-teal | <!-- bg:teal --> teal | <!-- bg:bold-teal --> bold-teal |
| <!-- bg:light-green --> light-green | <!-- bg:green --> green | <!-- bg:bold-green --> bold-green |
| <!-- bg:light-yellow --> light-yellow | <!-- bg:yellow --> yellow | <!-- bg:bold-yellow --> bold-yellow |
| <!-- bg:light-red --> light-red | <!-- bg:red --> red | <!-- bg:bold-red --> bold-red |
| <!-- bg:light-purple --> light-purple | <!-- bg:purple --> purple | <!-- bg:bold-purple --> bold-purple |

## Edge cases

A colored header cell, an off-palette hex, a marker-only (empty) cell, and
alignment alongside a color:

| <!-- bg:light-grey --> colored header | <!-- bg:#c0ffee --> off-palette hex |
| :--- | ---: |
| <!-- bg:grey --> | right-aligned, uncolored |
| <!-- bg:BOLD-Red --> shouty marker | <!-- bg:light-green --> aligned and colored |

These publish uncolored (each emits a warning): an unknown name, and a marker that
isn't first in its cell.

| Cell | Result |
| --- | --- |
| <!-- bg:chartreuse --> unknown name | no background |
| trailing marker <!-- bg:green --> | no background |
