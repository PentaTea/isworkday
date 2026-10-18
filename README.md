# isworkday

A command-line predicate that does what its name says: `isworkday cn && ./deploy.sh`.

> Only `cn` (mainland China) is supported so far.

Answers follow the official calendar, kept current online: next year's schedule needs no upgrade, and a date not yet announced is `unknown`, never a guess.

## Install

```sh
brew install pentatea/tap/isworkday
uvx isworkday cn                      # or: uv tool install isworkday
npx -y @isworkday/cli cn              # or: npm i -g @isworkday/cli
go install github.com/PentaTea/isworkday@latest
```

In a container or CI, take the binary directly:

```sh
curl -fsSL https://github.com/PentaTea/isworkday/releases/latest/download/isworkday-$(uname -s)-$(uname -m).tar.gz | tar -xzC /usr/local/bin isworkday
```

macOS builds need macOS 13 or later.

## Use

```console
$ isworkday cn
2026-10-19 Mon · weekday
true
$ isworkday cn 2026-10-10
2026-10-10 Sat · workday (国庆节 补班)
true
```
<!-- pypi: end -->

A date whose schedule isn't out yet:

```console
$ isworkday cn 2027-05-01
unknown
error: no data for 2027-05-01 yet — covered through 2026-12-28
```

The answer is the word on stdout and the exit code; the reason line goes to stderr. `tomorrow` works too.

`today` and `tomorrow` are dates in REGION (for `cn`, Asia/Shanghai, UTC+8), whatever the machine's time zone; when the local date differs, the reason line says so. To ask about the local date, pass it: `isworkday cn "$(date +%F)"`.

## Output

| stdout | exit | meaning | stderr |
|---|---|---|---|
| `true` | 0 | workday in REGION (`cn`: Mon–Fri, or a 补班 make-up day on a weekend) | reason line |
| `false` | 1 | holiday or weekend in REGION | reason line |
| `unknown` | 3 | no data for that date yet | `error: …` |
| *(empty)* | 2 | usage error | `error: …` |

## Data

Calendars ship separately from the binary: a GitHub Action in this repository publishes `data/<region>.json`. Each binary embeds a snapshot as a fallback, refreshes at most every 12 h into `$XDG_CACHE_HOME/isworkday` (default `~/.cache/isworkday`), and never steps back to older data.

- **Propagation**: a new year's schedule reaches every machine within a day of an upstream publishing it. A change to an already published year goes out once the upstreams agree or a maintainer adds a patch.
- **Offline**: answers come from the last known data, with a warning:
  ```
  warning: using data as of 2025-11-04 — refresh failed: cdn.jsdelivr.net: timeout after 3s; raw.githubusercontent.com: HTTP 429
  ```
- **`verified`**: when the pipeline last confirmed the upstreams agree, renewed at least every 30 days. Fetched data unverified for over 90 days warns:
  ```
  warning: data last verified 2026-07-01, over 90 days ago — the data pipeline may have stopped; see https://github.com/PentaTea/isworkday
  ```

### `cn`

Every 6 hours the Action merges [holiday-cn](https://github.com/NateScarlet/holiday-cn) and [chinese-days](https://github.com/vsme/chinese-days), MIT-licensed projects (see [NOTICE](NOTICE)) built from the notices of the General Office of the State Council (国务院办公厅), requires them to agree day by day, and publishes [`data/cn.json`](data/cn.json) at:

```
https://cdn.jsdelivr.net/gh/PentaTea/isworkday@main/data/cn.json
https://raw.githubusercontent.com/PentaTea/isworkday/main/data/cn.json   (fallback)
```

These URLs are a contract: the file stays schema 1.

## For AI agents

[SKILL.md](SKILL.md) is a ready-made skill: drop it into your agent's skills directory or instructions.

## Troubleshooting

- **Allowlisted network**: data needs `cdn.jsdelivr.net` (fallback `raw.githubusercontent.com`); the curl install needs `github.com` and `release-assets.githubusercontent.com`. `HTTPS_PROXY` and `NO_PROXY` are honored, `SSL_CERT_FILE` on Linux; macOS uses the system keychain.
- **Refresh fails with an `x509` error on a bare image**: install `ca-certificates`.
- **Internal mirror or fork**: `ISWORKDAY_DATA_URL=https://… isworkday cn` fetches from that URL only.
- **Force a refresh**: `rm -rf ~/.cache/isworkday`.

## Regions

- `cn` — mainland China, the State Council's nationwide schedule. Not included: stock-exchange trading days, regional holidays, half-day holidays for some groups (such as 妇女节).

## Report a wrong date

[Open an issue](https://github.com/PentaTea/isworkday/issues) with a link to the official notice. The fix is a [patch](pipeline/patches/README.md) and reaches installed binaries within a day, no upgrade needed.

## Contributing

PRs adding regions are welcome. A region takes an ISO 3166 code, a `data/<code>.json` built by an adapter in `pipeline/`, and a `region` like the one in [`cn.go`](cn.go) (UTC offset, weekend days). Cite the official source of the schedule.

## License

[MIT](LICENSE)
