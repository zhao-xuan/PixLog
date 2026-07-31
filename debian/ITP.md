# Intent to Package draft

Send this message from the maintainer address to `submit@bugs.debian.org` before
requesting sponsorship. Replace the changelog's initial-release line with
`Initial release. (Closes: #NNNNNN)` after the BTS assigns a bug number.

```text
From: Xuan Zhao <zhaoxuan0914@gmail.com>
To: submit@bugs.debian.org
Subject: ITP: pixlog -- Git-compatible provenance layer for image assets
X-Debbugs-CC: debian-devel@lists.debian.org

Package: wnpp
Severity: wishlist
Owner: Xuan Zhao <zhaoxuan0914@gmail.com>

* Package name    : pixlog
  Version         : 0.1.0
  Upstream Author : Xuan Zhao <zhaoxuan0914@gmail.com>
* URL             : https://github.com/zhao-xuan/PixLog
* License         : MIT
  Programming Lang: Go
  Description     : Git-compatible provenance layer for image assets

PixLog stores image media and provenance alongside Git while leaving commits,
branches, merges, and remotes under Git's control. It provides content-addressed
media storage, visual diffs, reproducible recipes, capture workflows, and
integrity checks for image assets.

I intend to maintain this package with the Debian Go Packaging Team and will
need sponsorship for the initial upload.
```