# Encoded Image Sources

A markdown image destination is a URL, not a path, so a filename with a space
has to be percent-encoded to be referenced at all. This is the spelling most
editors and previews produce, and the one this case exists for:

![percent-encoded](assets/my%20image.png)

The angle-bracket form is the same image by another spelling, and resolves to
the same attachment:

![angle brackets](<assets/my image.png>)

A bare space is not a valid destination, so this is not an image at all -- it
stays literal text, exactly as GitHub and a local preview render it. Nothing
is uploaded and nothing is reported broken, because no image was ever parsed:

![bare space](assets/my image.png)

Non-ASCII filenames encode the same way:

![accented](assets/caf%C3%A9.png)

A literal "%" in a filename is not an escape sequence. It is left as written,
so a file genuinely named "100%.png" still resolves:

![literal percent](assets/100%.png)

An ordinary path is unaffected:

![plain](assets/plain.png)

An encoded "../" is decoded before the documentation-root check runs, so the
encoding cannot slip an escaping path past it -- compare the plain spelling in
the images-broken case, which is refused for the same reason:

![encoded traversal](..%2F..%2F..%2F..%2F..%2F..%2F..%2F..%2Fetc%2Fpasswd.png)
