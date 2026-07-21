# Raw Confluence Storage

A pasted status macro passes straight through:

<ac:structured-macro ac:name="status" ac:schema-version="1">
<ac:parameter ac:name="colour">Green</ac:parameter>
<ac:parameter ac:name="title">Done</ac:parameter>
</ac:structured-macro>

A layout whose cells contain markdown that still gets converted:

<ac:layout>
<ac:layout-section ac:type="two_equal">
<ac:layout-cell>

Left column with **bold** and a [link](https://example.net).

</ac:layout-cell>
<ac:layout-cell>

Right column with a list:

- one
- two

</ac:layout-cell>
</ac:layout-section>
</ac:layout>

Storage format inside a code fence stays literal:

```
<ac:structured-macro ac:name="info"/>
```
