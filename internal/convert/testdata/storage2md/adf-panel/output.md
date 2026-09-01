An editor-authored purple Note panel renders once, from its node:

<ac:adf-extension>
<ac:adf-node type="panel">
<ac:adf-attribute key="panel-type">note</ac:adf-attribute>
<ac:adf-content>

**Notes / open questions**

- An *emphasised* item with `code`.
- A second item.

</ac:adf-content>
</ac:adf-node>
</ac:adf-extension>

An extension of some other type passes through the same way:

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
