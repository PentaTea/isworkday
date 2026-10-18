---
name: isworkday
description: Check whether a date is a statutory workday in a region, with official holidays and make-up workdays applied. Only cn (mainland China) so far. Use before running anything that should only happen on a workday.
---

# isworkday

One command, one word, one exit code. Only `cn` (mainland China) so far.

## Call it

    isworkday cn              # today, on the region's date (cn: Asia/Shanghai)
    isworkday cn tomorrow
    isworkday cn 2026-10-10   # strict YYYY-MM-DD

Not installed? `uvx isworkday cn …` · `npx -y @isworkday/cli cn …` ·
`curl -fsSL https://github.com/PentaTea/isworkday/releases/latest/download/isworkday-$(uname -s)-$(uname -m).tar.gz | tar -xzC /usr/local/bin isworkday`

## Read the answer

- stdout `true` + exit 0 → workday. **Only exit 0 means go.**
- stdout `false` + exit 1 → holiday or weekend; a normal answer, not an error.
- stdout `unknown` + exit 3 → no data for that date yet; tell the user.
- exit 2, empty stdout → usage error.
- stderr: a reason line such as `2026-10-10 Sat · workday (国庆节 补班)`, sometimes after a `warning:` line about stale data.

`today` is the region's date (for `cn`, Beijing's), not the machine's; for the local date, pass it: `isworkday cn "$(date +%F)"`.

## Reference (`isworkday --help`)

<!-- isworkday --help -->
```text
isworkday — is DATE a statutory workday in REGION?

Usage: isworkday REGION [DATE]
  REGION   cn (mainland China)
  DATE     YYYY-MM-DD, today (default) or tomorrow; today/tomorrow are dates in REGION.

Output: one word on stdout + exit code, a one-line reason on stderr.
  true      0   workday (a weekday, or a make-up day on a weekend)
  false     1   holiday or weekend
  unknown   3   no data for that date yet
  (empty)   2   usage error

Data: official calendars via https://github.com/PentaTea/isworkday, refreshed
  at most every 12 h into $XDG_CACHE_HOME/isworkday (~/.cache); offline, the
  last known data answers with a warning.

Options:
  -h, --help      show this help
  -V, --version   show version and data coverage

Environment:
  ISWORKDAY_DATA_URL   fetch data from this URL instead
```
