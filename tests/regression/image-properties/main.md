# Image Properties

A JSON title sets title/width/height/align attributes:

![sized shot](assets/shot.png '{"title": "A Tooltip", "width": "300", "height": "150", "align": "center"}')

A plain-string title becomes a tooltip (`ac:title`):

![tooltip shot](assets/shot.png "Just a tooltip")

Invalid width/align values are dropped with warnings:

![bad props](assets/shot.png '{"width": "wide", "align": "middle"}')
