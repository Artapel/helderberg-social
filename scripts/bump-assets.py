#!/usr/bin/env python3
"""Stamp ?v=<timestamp> on every local CSS/JS reference in the static pages.

GitHub Pages serves assets with a 10-minute cache and browsers hold them
longer, so a deploy that changes site.js or site.css is invisible to
returning visitors until the reference changes. Run this before committing a
change to anything under assets/ or data/:

    python3 scripts/bump-assets.py
"""
import datetime, glob, re

stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%d%H%M")
pat = re.compile(r'((?:src|href)=")(/?(?:assets/(?:js|css)/[^"?]+|data/data\.js))(?:\?v=\d+)?(")')
changed = 0
for path in glob.glob("*.html"):
    s = open(path, encoding="utf-8").read()
    n = pat.sub(lambda m: m.group(1) + m.group(2) + "?v=" + stamp + m.group(3), s)
    if n != s:
        open(path, "w", encoding="utf-8", newline="\n").write(n)
        changed += 1
print("stamped v=%s in %d pages" % (stamp, changed))
