<!-- confluence-toc -->

# Overview

This page exercises markfluence's conversion features so you can eyeball the
published result. It has **bold**, _italic_, `inline code`, and a
[link to Atlassian](https://www.atlassian.com).

<!-- markfluence-version -->

- A bullet
- Another bullet
  - Nested bullet
1. Ordered one
2. Ordered two

## GitHub-style callout

> [!NOTE]
> This is a GitHub-style callout; markfluence converts it to a Confluence panel.

## Code block

```python
def hello(name):
    print(f"hello, {name}")
```

## Table

| Feature   | Works? |
| --------- | ------ |
| Tables    | yes    |
| Callouts  | yes    |

# Raw Confluence storage format

Everything below is pasted storage format (`ac:` elements), emitted verbatim.

## Info panel with converted markdown body

<ac:structured-macro ac:name="info" ac:schema-version="1">
<ac:rich-text-body>

This is an **info** panel. The body is markdown — note the blank lines around it —
so this [link](https://example.com) and list convert:

- alpha
- beta

</ac:rich-text-body>
</ac:structured-macro>

## Status macro (parameters, no body)

<ac:structured-macro ac:name="status">
<ac:parameter ac:name="colour">Green</ac:parameter>
<ac:parameter ac:name="title">DONE</ac:parameter>
</ac:structured-macro>

## Expand macro

<ac:structured-macro ac:name="expand" ac:schema-version="1">
<ac:parameter ac:name="title">Click to expand</ac:parameter>
<ac:rich-text-body>

Hidden **markdown** content, revealed on click.

</ac:rich-text-body>
</ac:structured-macro>

## Two-column layout

<ac:layout>
<ac:layout-section ac:type="two_equal">
<ac:layout-cell>

### Left column

Some **markdown** in the left cell, with a
[link](https://developer.atlassian.com).

</ac:layout-cell>
<ac:layout-cell>

### Right column

- first
- second

</ac:layout-cell>
</ac:layout-section>
</ac:layout>

## A storage example in a code fence (should stay literal, not activate)

```xml
<ac:structured-macro ac:name="info"/>
```

# Images

A **local** image (uploaded as a page attachment):

![Local test image](assets/markfluence-test.png)

The **same local image sized and centered** via the JSON-in-title properties:

![Sized local image](assets/markfluence-test.png '{"width":"64","align":"center"}')

A **remote** image (referenced by URL, not attached):

![Remote placeholder](https://placehold.co/150x60/png)
