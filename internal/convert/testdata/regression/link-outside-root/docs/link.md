# Escaping Link

A [link to a page outside the documentation root](../outside/linked.md) is
left exactly as written -- unresolved, and reported (minimal R1) -- because
the target sits above root, so the link index never walked it. Making
resolution path-aware makes this a real path lookup rather than accidentally
safe by basename flattening (025's Scenario F); it still needs no clamp,
since a path outside root is simply never in the index in the first place.
