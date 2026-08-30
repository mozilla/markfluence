# Escaping Link

A [link to a page outside the documentation root](../outside/linked.md) is
Broken: it publishes as literal "LINK BROKEN: ... (outside the documentation
root)" text in place of the link element and its visible text, because the
target sits above root, so the link index never walked it. The query side
tells this apart from a genuine "not found" via a lexical escape check
(025's Scenario F); the index itself still needs no clamp, since a path
outside root is simply never in the index in the first place.
