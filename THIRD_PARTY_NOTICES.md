# Protocol References

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
