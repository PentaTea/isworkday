# Patches

A patch is a maintainer's ruling on specific dates, applied on top of the merged upstreams ([holiday-cn](https://github.com/NateScarlet/holiday-cn) and [chinese-days](https://github.com/vsme/chinese-days)). Add one when the data workflow fails with `conflict <date>: holiday-cn=… chinese-days=…`, or when both upstreams are wrong. Read the notice on [gov.cn](https://www.gov.cn) first.

Then report the error upstream ([holiday-cn](https://github.com/NateScarlet/holiday-cn/issues), [chinese-days](https://github.com/vsme/chinese-days/issues)) with the gov.cn link, and put the issue URL in the patch's `note`.

## Format

`pipeline/patches/<YYYY-MM-DD>-<slug>.json`, named after the notice's date; patches apply in file name order. Example: the 2020 Spring Festival extension, which also cancelled the 补班 make-up day on Saturday, February 1:

```json
{
  "source": "https://www.gov.cn/zhengce/zhengceku/2020-01/27/content_5472352.htm",
  "note": "2020 春节延长至 2 月 2 日",
  "holidays": { "2020-01-31": "春节" },
  "workdays": {},
  "default": ["2020-02-01"]
}
```

| Key | Dates it takes | Effect |
|---|---|---|
| `holidays` | Mon–Fri | A day off, with the festival name shown in the reason line |
| `workdays` | Sat/Sun | A 补班 make-up working day, with the festival name |
| `default` | any | Back to plain Mon–Fri (taken out of both tables) |
| `source`, `note` | — | Stay in this file and are not published |

The workflow rejects a patch, and publishes nothing, when a date is listed twice or lies outside `range` in `data/cn.json`, a holiday falls on a weekend or a workday on a weekday, or a key is unknown.

## Removing a patch

Once the upstreams agree with a patch, the workflow log says:

```
note: patches/2020-01-27-spring-festival.json is redundant (upstreams agree) — delete it
```

Delete it then, so that a later upstream correction is not hidden behind it.

## Trying it locally

```sh
git clone --depth 1 https://github.com/NateScarlet/holiday-cn /tmp/holiday-cn
go run ./pipeline -hcn /tmp/holiday-cn
```
