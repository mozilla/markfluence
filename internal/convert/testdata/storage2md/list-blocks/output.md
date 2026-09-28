1. add a comment; something like
   ```
   Thanks for your request.
   ```
2. set the status to “IN PROGRESS”

The shapes a code block takes:

- the editor's paragraph form
  ```bash
  echo one

  echo two
  ```
- text before
  ```
  middle
  ```
  text after
- ```
  only a code block
  ```
- a code block, then a nested list
  ```
  code
  ```
  - nested
    ```
    nested code
    ```
- a status stays inline <ac:structured-macro ac:name="status" ac:schema-version="1"><ac:parameter ac:name="title">DONE</ac:parameter></ac:structured-macro>

Blocks other than a code block:

- a callout

  > [!NOTE]
  > note this
- a quote, then a nested list

  > quoted

  - nested
- a table

  | a | b |
  | --- | --- |
  | 1 | 2 |

Order and separation:

- a nested list before a code block
  - n
  ```
  after the list
  ```
- a nested list before text
  - n

  text after the list
- a table Markdown cannot express, then a code block

  <table>
  <tbody>
  <tr>
  <td>

  x

  </td>
  </tr>
  </tbody>
  </table>

  ```
  after the table
  ```
- ```
  first
  ```
  a  
  b
- a  
  b
