# Protocol References

The 115 Open offline integration independently implements the URL batch,
torrent parsing, and BT submission HTTP fields observed in the local LitePan
`drivers/115_Open/offline_download.go`. LitePan uses the PolyForm
Noncommercial License 1.0.0. No LitePan source file or runtime dependency is
bundled; Aether uses its own upload, authentication, validation, directory
selection, response handling, and tests. Real-account interoperability remains
unverified. The inspected LitePan Quark driver and OpenList `quark_uc/driver.go`
did not provide a native offline protocol; Quark submission is explicitly unavailable.

The Tianyi personal-cloud integration independently implements the HTTP login,
session signature, listing, download, rapid-upload and batch-delete protocol
documented by the behavior of OpenList's `drivers/189pc` at revision
`4c39bbe9c228680e2a6f78555175f7f2063d452c`
(https://github.com/OpenListTeam/OpenList). OpenList is licensed under
AGPL-3.0; no OpenList source files or runtime dependencies are bundled.
Aether's integration uses the Go standard library and its own state, validation,
credential handling, CAS journal and tests. It does not implement OpenList's
interactive captcha/SMS verification or torrent CAS extensions.

The native Mobile Cloud (139) and CAS implementation was developed with reference
to the local 139Strm source (`yun139/client.py`, `crypto.py`, `cas.py`, and
`strm.py`). Aether implements its own Go integration, signed playback claims,
encrypted temporary-file journal, cancellation, and cleanup policy.

139Strm includes the following license:

MIT License

Copyright (c) 2026 tianjian

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

139Strm's README also credits OpenList and community CAS implementations as
protocol references. No OpenList source files were copied into this change.

## AetherLink local integration (2026-10-05)

At the user's explicit request, the media proxy core and its tests were imported
from `C:/Develop/AetherLink/internal` into `internal/linkcore`: config, logx,
pathmap, proxy, resolver, stats, strm, upstream and urlx. Internal import paths
were mechanically rewritten; the original comments and tests are retained.
Integration changes add a shared cancellable audio cache, M4B conversion, active
audio protection, FFmpeg protocol restrictions and the Aether logging sink.

`web/public/media/abs.png`, `emby.png` and `fnmovie.png` come from that local
project's `web/public/icons`. They identify their respective media products;
ownership remains with the original owners.

No LICENSE file was present in the supplied AetherLink checkout. No open-source
license grant is inferred here; this provenance record does not replace
permission from the original rights holders.

## Cloud file operation protocol references

`internal/app/cloud_file_actions.go` independently implements request fields and
endpoint flows inspected in the user-supplied local LitePan checkout:
`drivers/139Cloud/{transport,ops}.go`, `drivers/189Cloud/ops.go`,
`drivers/115_Open/{transport,ops}.go`, and `drivers/Quark/{transport,ops}.go`.
No LitePan service implementation or framework code is incorporated. Native
provider authorization and transport remain Aether's existing drivers.

# rclone

Docker image installs the Alpine rclone package (MIT license) as a separate FUSE
mount process. Aether communicates with it through a private loopback WebDAV
bridge; no rclone source code is copied into this repository.
Upstream: https://github.com/rclone/rclone

## In-process offline downloads

HTTP downloads use Go's standard library. BitTorrent downloads link
`github.com/anacrolix/torrent` v1.61.0 (MPL-2.0), with its transitive dependencies
under their respective licenses. Upstream: https://github.com/anacrolix/torrent .
No upstream library source files are modified. aria2 is no longer included.
`internal/app/builtin_{offline,http,torrent}.go` independently implement the
download-then-upload workflow after inspecting the supplied LitePan
`internal/offlinedownload` directory. No LitePan implementation was copied.

## 115 Cookie driver and CAS protocol reference (2026-10-07)

Aether links `github.com/SheltonZhu/115driver` v1.3.5, unmodified, for
official QR Cookie login, files, uploads and native offline downloads.
Upstream: https://github.com/SheltonZhu/115driver .
Its author expresses opposition to use by AlistGo, while explicitly noting
that the MIT license does not impose that restriction and that the opposition
does not apply to community forks or other users respecting privacy.
The former LitePan OAuth service and 115 Open API paths are no longer used.

MIT License

Copyright (c) 2022-2024 SheltonZhu

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

The supplied OpenList checkout's `drivers/189/torrent.go` and
`pkg/torrent/torrent.go` were inspected for CAS hash fields and the Tianyi
multi-upload protocol. Aether independently computes slice hashes and keeps
its existing CAS JSON envelope; no OpenList source files were copied.
