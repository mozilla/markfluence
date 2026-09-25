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

A raw table passes through with every attribute, including a blank line
inside it. A cell body set off by blank lines is Markdown; one tight against
its tags stays literal:

<table data-layout="center" data-table-width="900">
<colgroup>
<col style="width: 300px;" />
<col style="width: 600px;" />
</colgroup>
<tbody>
<tr>
<th colspan="2">

Q3 **results**

</th>
</tr>

<tr>
<td rowspan="2" data-highlight-colour="#e3fcef">

**auth** is [up](https://example.net)

</td>
<td><p style="text-align: right;">tight **not markdown**</p></td>
</tr>
<tr>
<td>99.8%</td>
</tr>
</tbody>
</table>

Storage format inside a code fence stays literal:

```
<ac:structured-macro ac:name="info"/>
```
