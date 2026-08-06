---
title: markfluence table storage probe
space: ~60c36d0718e9f60071326951
parent: 76646878
page_id: 2913796912
page_width: max
---

Each table below is hand-written raw storage testing one hypothesis about
Confluence's table attributes. Everything is pasted storage (no blank lines inside
a table, so each stays a single CommonMark HTML block and passes through verbatim).

## T1 — bare table, no attributes (baseline)

<table><tbody><tr><th><p>T1 head A</p></th><th><p>T1 head B</p></th></tr><tr><td><p>bare</p></td><td><p>no attrs at all</p></td></tr></tbody></table>

## T2 — data-layout="center", no data-table-width

<table data-layout="center"><tbody><tr><th><p>T2 head A</p></th><th><p>T2 head B</p></th></tr><tr><td><p>layout=center</p></td><td><p>no width attr</p></td></tr></tbody></table>

## T3 — data-layout="align-start", no data-table-width

<table data-layout="align-start"><tbody><tr><th><p>T3 head A</p></th><th><p>T3 head B</p></th></tr><tr><td><p>layout=align-start</p></td><td><p>no width attr</p></td></tr></tbody></table>

## T4 — data-layout="wide" (undocumented guess)

<table data-layout="wide"><tbody><tr><th><p>T4 head A</p></th><th><p>T4 head B</p></th></tr><tr><td><p>layout=wide</p></td><td><p>is this value accepted?</p></td></tr></tbody></table>

## T5 — data-layout="full-width" (undocumented guess)

<table data-layout="full-width"><tbody><tr><th><p>T5 head A</p></th><th><p>T5 head B</p></th></tr><tr><td><p>layout=full-width</p></td><td><p>is this value accepted?</p></td></tr></tbody></table>

## T6 — align-start + data-table-width="1110" (known-good control)

<table data-layout="align-start" data-table-width="1110"><tbody><tr><th><p>T6 head A</p></th><th><p>T6 head B</p></th></tr><tr><td><p>layout+width</p></td><td><p>copied from the editor-authored page</p></td></tr></tbody></table>

## T7 — colgroup in percentages instead of px

<table data-layout="align-start" data-table-width="1110"><colgroup><col style="width: 25%;" /><col style="width: 75%;" /></colgroup><tbody><tr><th><p>T7 head A</p></th><th><p>T7 head B</p></th></tr><tr><td><p>25%</p></td><td><p>75% — do percentage widths work?</p></td></tr></tbody></table>

## T8 — colgroup px with no data-table-width

<table data-layout="align-start"><colgroup><col style="width: 200.0px;" /><col style="width: 600.0px;" /></colgroup><tbody><tr><th><p>T8 head A</p></th><th><p>T8 head B</p></th></tr><tr><td><p>200px</p></td><td><p>600px, but no data-table-width</p></td></tr></tbody></table>

## T9 — cell background: hex, named, and on a th

<table data-layout="align-start" data-table-width="1110"><tbody><tr><th data-highlight-colour="#deebff"><p>T9 head, hex on th</p></th><th><p>T9 head B</p></th></tr><tr><td data-highlight-colour="#ffebe6"><p>hex #ffebe6</p></td><td data-highlight-colour="grey"><p>named "grey" (Server-era spelling)</p></td></tr></tbody></table>

## T10 — what markfluence emits today, plus legacy align attributes

<table><thead><tr><th>T10 head A</th><th align="right">T10 head B</th></tr></thead><tbody><tr><td>thead, no p-wrappers</td><td align="right">align="right" on the td</td></tr></tbody></table>

## T11 — text-align on a header cell's paragraph

<table data-layout="align-start" data-table-width="1110"><tbody><tr><th><p style="text-align: right;">T11 right-aligned head</p></th><th><p style="text-align: center;">T11 centered head</p></th></tr><tr><td><p>does header alignment</p></td><td><p>survive?</p></td></tr></tbody></table>

## T12 — numbered column (isNumberColumnEnabled guesses)

<table data-layout="align-start" data-table-width="1110" data-number-column="true"><tbody><tr><th><p>T12 head A</p></th><th><p>T12 head B</p></th></tr><tr><td><p>data-number-column</p></td><td><p>="true"</p></td></tr></tbody></table>

## T13 — control: an ordinary GFM table through the converter

| Feature | Works? |
| :------ | -----: |
| GFM     |    yes |

## T14 — percentage colgroup with NO data-table-width

<table data-layout="align-start"><colgroup><col style="width: 30%;" /><col style="width: 70%;" /></colgroup><tbody><tr><th><p>T14 head A</p></th><th><p>T14 head B</p></th></tr><tr><td><p>30%</p></td><td><p>70%, no data-table-width — does % resolve?</p></td></tr></tbody></table>

## T15 — full-width with percentage colgroup

<table data-layout="full-width"><colgroup><col style="width: 20%;" /><col style="width: 80%;" /></colgroup><tbody><tr><th><p>T15 head A</p></th><th><p>T15 head B</p></th></tr><tr><td><p>20%</p></td><td><p>80% under full-width</p></td></tr></tbody></table>

## T16 — full-width + data-table-width + percentage colgroup

<table data-layout="full-width" data-table-width="1110"><colgroup><col style="width: 20%;" /><col style="width: 80%;" /></colgroup><tbody><tr><th><p>T16 A (20%)</p></th><th><p>T16 B (80%)</p></th></tr><tr><td><p>narrow col</p></td><td><p>wide col — are the 20/80 proportions honored under full-width?</p></td></tr></tbody></table>

## T17 — full-width + data-table-width + px colgroup

<table data-layout="full-width" data-table-width="1110"><colgroup><col style="width: 200.0px;" /><col style="width: 910.0px;" /></colgroup><tbody><tr><th><p>T17 A (200px)</p></th><th><p>T17 B (910px)</p></th></tr><tr><td><p>narrow col</p></td><td><p>wide col — px proportions under full-width?</p></td></tr></tbody></table>

## T18 — wide + data-table-width + percentage colgroup (control)

<table data-layout="wide" data-table-width="1110"><colgroup><col style="width: 20%;" /><col style="width: 80%;" /></colgroup><tbody><tr><th><p>T18 A (20%)</p></th><th><p>T18 B (80%)</p></th></tr><tr><td><p>narrow col</p></td><td><p>wide col — same, but layout=wide</p></td></tr></tbody></table>

## T19 — full-width, NO data-table-width, px colgroup

<table data-layout="full-width"><colgroup><col style="width: 200.0px;" /><col style="width: 910.0px;" /></colgroup><tbody><tr><th><p>T19 A (200px)</p></th><th><p>T19 B (910px)</p></th></tr><tr><td><p>px widths</p></td><td><p>no data-table-width, full-width layout</p></td></tr></tbody></table>

## T20 — wide, NO data-table-width, px colgroup

<table data-layout="wide"><colgroup><col style="width: 200.0px;" /><col style="width: 910.0px;" /></colgroup><tbody><tr><th><p>T20 A (200px)</p></th><th><p>T20 B (910px)</p></th></tr><tr><td><p>px widths</p></td><td><p>no data-table-width, wide layout</p></td></tr></tbody></table>

## T21 — bare table, NO layout, NO data-table-width, px colgroup

<table><colgroup><col style="width: 200.0px;" /><col style="width: 910.0px;" /></colgroup><tbody><tr><th><p>T21 A (200px)</p></th><th><p>T21 B (910px)</p></th></tr><tr><td><p>px widths</p></td><td><p>no layout at all</p></td></tr></tbody></table>
