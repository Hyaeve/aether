# Browser media fixtures

`thumbnail.webm` is a generated two-second VP8 video, 320x180 at 15 fps. It has
a dark blue background and a green rectangle; it contains no external media.
The stable background at (200,20) is used to verify real decoded thumbnail
pixels at DPR 1, 2 and 3, independently of MediaRecorder timing in CI.

Regenerate from the repository root with FFmpeg (remove the old fixture first
only when intentionally regenerating it):

```powershell
ffmpeg -hide_banner -loglevel error -n -f lavfi -i "color=c=0x174b64:s=320x180:r=15:d=2,drawbox=x=20:y=35:w=90:h=100:color=0xb7e4bd:t=fill" -an -c:v libvpx -pix_fmt yuv420p -b:v 100k -map_metadata -1 web/tests/fixtures/thumbnail.webm
```
