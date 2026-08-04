# Table Cell Colors

A leading `bg:` comment sets a cell background, by swatch name or hex, in body
cells and header cells alike:

| Service | <!-- bg:light-grey --> Status  | Notes                      |
| :------ | :---------------------------- | -------------------------: |
| auth    | <!-- bg:light-green -->ok     | steady                     |
| billing | <!--bg:light-red--> down      | <!-- bg:#fffae6 --> paging |
| search  | <!-- bg:grey -->              | decommissioned             |

An unknown color name is dropped with a warning, and so is a marker that isn't
first in its cell:

| Cell                        | Result                     |
| --------------------------- | -------------------------- |
| <!-- bg:chartreuse --> nope | no background              |
| trailing <!-- bg:green -->  | no background              |

The keyword and the color are case-insensitive; other comments pass through
untouched:

| Cell                            |
| ------------------------------- |
| <!-- BG:Bold-Blue --> shouty    |
| <!-- bg:#FFEBE6 --> shouty hex  |
| <!-- a note --> text            |
