A purple panel is IMPORTANT, read from its node -- its fallback holds the same content and must not render too:

> [!IMPORTANT]
> **Notes / open questions**
>
> - An *emphasised* item with `code`.
> - A second item.

An extension of some other type has no markdown spelling, so it passes through -- node only:

<ac:adf-extension>
<ac:adf-node type="expand">
<ac:adf-attribute key="title">Details</ac:adf-attribute>
<ac:adf-content>

Hidden **body**.

</ac:adf-content>
</ac:adf-node>
</ac:adf-extension>

An extension with a fallback and no node keeps the fallback, its only copy:

<ac:adf-extension>
<ac:adf-fallback>
<div class="panel">
<p>Only copy.</p>
</div>
</ac:adf-fallback>
</ac:adf-extension>
