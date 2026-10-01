# Contributing to Leafbind

Thank you for your interest in Leafbind. Bug reports, ideas and pull
requests are all welcome.

## Reporting a problem

Open an [issue](https://github.com/abdulwaheedsyed/leafbind/issues) with the
Leafbind version (`leafbind --version`), your operating system, and what
you did, what happened and what you expected. If a particular PDF causes the
problem, say what kind it is (a scan, a slide deck, a typeset book) and how
many pages it has; attach it only if you are free to share it publicly.

Do not report security vulnerabilities in public issues; see
[SECURITY.md](SECURITY.md).

## Making a change

For anything beyond a small fix, open an issue first, so we can agree on
the approach before you spend time on it.

1. Fork the repository and create a branch from `main`.
2. Make your change. Keep to the style of the surrounding code: `gofmt`,
   comments that explain why rather than what, and small, focused commits.
3. Add or update tests. The end-to-end tests generate their own PDFs, so no
   sample documents are needed; see the Development section of the
   [README](README.md#development).
4. Run the checks:

   ```bash
   gofmt -l .        # prints nothing when everything is formatted
   go vet ./...
   make test
   ```

   If you change dependencies, run `make notices` to regenerate
   `THIRD_PARTY_NOTICES.md`; the tests fail until you do.
5. Sign off every commit (see below), push your branch and open a pull
   request that explains what the change does and why. It can be merged
   once every CI check passes: the tests on each platform, the native
   window, the comparison with EPUBCheck, and the sign-off check.

New source files start with the same two-line header as the others:

```go
// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0
```

## Developer Certificate of Origin

Leafbind uses the [Developer Certificate of Origin](DCO) (DCO), as the
Linux kernel and many other projects do. By signing off a commit, you
certify that you wrote the change, or otherwise have the right to submit
it, under the project's license.

Sign off by adding a line to the end of each commit message, with your real
name and an email address you can be reached at:

```text
Signed-off-by: Your Name <you@example.com>
```

`git commit -s` adds it for you. To sign off commits you have already made
on your branch, run `git rebase --signoff main` and force-push the branch.
A check on every pull request makes sure each commit is signed off.

## Notes for maintainers

- Dependabot opens weekly pull requests for Go modules and GitHub Actions.
  A Go update fails the notices test until the notices are regenerated:
  check out its branch, run `make notices`, and push the result. A
  go-pdfium update can also change the PDFium build it embeds; see
  `third_party/licenses/README.md`.
- Actions are pinned to commit hashes, with the version in a comment, and
  Dependabot keeps both up to date.
- `main` cannot be deleted or force-pushed, by anyone. Other changes reach
  it through pull requests that pass CI; repository admins may push
  directly. Release tags cannot be moved or deleted except by an admin.
- CodeQL scans the Go code, the page's JavaScript and the workflows on every
  push; secret scanning blocks pushes that contain credentials. Findings are
  under the repository's Security tab.
- Releases are built, signed and published by the release workflow when a
  `v*` tag is pushed. Each archive gets a build provenance attestation,
  which anyone can check with
  `gh attestation verify <archive> --repo abdulwaheedsyed/leafbind`.

## License

Leafbind is licensed under the [Apache License 2.0](LICENSE). By
contributing, you agree that your contributions are licensed under it too,
as section 5 of the license provides, and you keep the copyright to them.

## Code of conduct

Everyone taking part in the project is expected to follow the
[code of conduct](CODE_OF_CONDUCT.md).
