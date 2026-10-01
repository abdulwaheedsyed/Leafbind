# Changelog

All notable changes to Leafbind. Versions follow
[semantic versioning](https://semver.org/); until 1.0, a minor version may
change behaviour.

## Unreleased

- Release archives carry signed build provenance attestations, which
  `gh attestation verify` checks; the release notes come from this
  changelog.
- The Windows version details no longer wrap round a version part above
  65535, and the schema reader refuses `\x{...}` escapes beyond the last
  Unicode code point.

## v0.9.0

- **License changed to the Apache License 2.0.** Leafbind was published
  under the MIT License before this release. The license now includes an
  explicit patent grant and keeps the Leafbind name and logo out of it; see
  [NOTICE](NOTICE) and [TRADEMARKS.md](TRADEMARKS.md).
- Earlier releases are withdrawn, and retracted in `go.mod`, so the Go tools
  steer away from them.
- Contribution guidelines with the Developer Certificate of Origin, a code
  of conduct and a security policy.

## Earlier versions

These were published under the MIT License, and have been withdrawn.

### v0.8.0

- `--split` divides each spread at its gutter, the fold of a scan or the gap
  between the pages, rather than at the centre.
- Each book in the interface keeps its own settings.

### v0.7.0

- A native window on Windows (WebView2) and macOS (WKWebView); in it, books
  are saved to the Downloads folder.
- Navigation and package documents are checked against EPUBCheck's schemas
  and Schematron rules.
- `--trim` crops empty margins; `--split` splits two-page spreads.
- `--out` converts several PDFs in one run.
- An installed epubcheck runs only with `--epubcheck`.

### v0.6.0

- `--pages` converts a page range; `--password` opens encrypted PDFs, and
  the interface asks for the password.
- The Windows executable carries its icon and version details; macOS has
  `Leafbind.app`; Linux has a desktop entry and icons.

### v0.5.0

- The table of contents comes from the PDF's bookmarks, and a page list from
  its page labels.
- A page preview in the interface, on a Kindle-sized screen.

### v0.4.0

- HTML content models are checked against EPUBCheck's XHTML schema, with a
  RELAX NG validator written for Leafbind.

### v0.3.1

- `--version` reports the module version in `go install` builds.

### v0.3.0

- Built-in EPUB validation equivalent to EPUBCheck, and `--check`.
- A logo and screenshots.

### v0.2.0

- Renamed to Leafbind.
- A graphical interface, and automatic resolution for scans.

### v0.1.0

- The first release: a command-line converter from PDF to Kindle
  fixed-layout EPUB, with its own JPEG encoder.
