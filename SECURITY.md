# Security policy

## Supported versions

Security fixes are made in the latest release only. Please make sure the
problem still happens with the newest version from the
[releases page](https://github.com/abdulwaheedsyed/leafbind/releases).

## Reporting a vulnerability

Please do not open a public issue. Report it privately instead, through
GitHub's [private vulnerability reporting](https://github.com/abdulwaheedsyed/leafbind/security/advisories/new),
or by email to abdulwaheedsyed@gmail.com.

Include what the problem is, how to reproduce it, and what an attacker could
do with it. You will hear back within a week. Once the problem is
confirmed, a fix is prepared and released, and you are credited in the
release notes unless you would rather not be.

## Scope

Leafbind reads untrusted input, PDFs and EPUBs, and its graphical interface
runs a local web server. Problems that are in scope include, for example:

- a PDF or EPUB that makes Leafbind run code, write files outside the
  output it was asked for, or read files it was not given;
- a way for another user of the machine, or a web page open in the
  browser, to reach the interface's local server;
- a way to make Leafbind hang or exhaust memory with a small input, beyond
  what the size of the document explains.
