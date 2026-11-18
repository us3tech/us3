# us3.tech

Static site built with [Hugo](https://gohugo.io) (extended, tested with v0.170.0).
English at the root, German under `/de/`. Design: `docs/website-design.md`.

    make site        # builds into site/public
    go test ./site/  # needs hugo; skipped under -short
    hugo server --source site   # local preview

`make site` takes the spec commit hashes from git
(`HUGO_PARAMS_SPECCOMMIT_EN/DE`). The download links come from `release.json`,
fetched from `params.releaseURL` in `hugo.toml` at build time (empty means no
fetch). To test locally, put a `release.json` (as written by `make release`)
into `site/data/`; it is git-ignored.

## Deployment

`.github/workflows/site.yml` builds with Hugo extended 0.170.0 (pinned SHA-256)
and deploys to GitHub Pages on pushes to `main` touching `site/` or `spec/`, on
published releases, and on manual dispatch. `static/CNAME` holds `us3.tech`.

## Fonts

Self-hosted WOFF2 under `static/fonts/` (SIL OFL 1.1, see `OFL.txt`), taken from
the Fontsource packages, subsets `latin` and `latin-ext` only:

| Package | Version | Weights |
|---|---|---|
| `@fontsource/montserrat` | 5.3.0 | 600, 700, 800 |
| `@fontsource/ibm-plex-sans` | 5.3.0 | 400, 500, 600 |
| `@fontsource/ibm-plex-mono` | 5.3.0 | 400, 500 |

The `@font-face` rules in `assets/css/site.css` use the packages' unicode ranges.

## Brand assets

`assets/brand/us3-mark.svg` and `us3-wordmark.svg` are cut from
`gfx/US3-logo_for_dark_bg_1000px.svg` (outlines only).
