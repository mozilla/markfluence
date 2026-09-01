# ADF Extension Passthrough

An ADF extension exported from a page republishes unchanged, with its body
converted back from markdown:

<ac:adf-extension>
<ac:adf-node type="expand">
<ac:adf-attribute key="title">Details</ac:adf-attribute>
<ac:adf-content>

Hidden **body** with a [link](https://example.net).

- one
- two

</ac:adf-content>
</ac:adf-node>
</ac:adf-extension>

No `ac:adf-fallback` is written back: Confluence regenerates the rendering it
held.
