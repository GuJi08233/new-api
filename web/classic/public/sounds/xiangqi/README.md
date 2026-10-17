# Xiangqi sound effects

These two sound effects are by **Kenney Vleugels (Kenney.nl)**, from
[Casino Audio 1.1](https://kenney.nl/assets/casino-audio). They are licensed under
[Creative Commons Zero 1.0 (CC0)](https://creativecommons.org/publicdomain/zero/1.0/).
The original package's license is preserved verbatim in [LICENSE-Kenney.txt](LICENSE-Kenney.txt).

Downloaded on 2026-10-17 from the
[official Kenney archive](https://kenney.nl/media/pages/assets/casino-audio/2472606a04-1721639069/kenney_casino-audio.zip).
Only the two sounds used by the game are included.

| Game file     | Original archive file       | Original title  | Use                            | Duration | Size         |
| ------------- | --------------------------- | --------------- | ------------------------------ | -------- | ------------ |
| `move.wav`    | `Audio/chip-lay-1.ogg`      | `chipLay1`      | A piece is placed on the board | 0.170 s  | 29,960 bytes |
| `capture.wav` | `Audio/chips-collide-1.ogg` | `chipsCollide1` | A piece captures another piece | 0.256 s  | 45,176 bytes |

The source Ogg Vorbis recordings were decoded to 16-bit PCM WAV, retaining their
44,100 Hz sample rate, two channels, and original volume. WAV was chosen for
browser compatibility, including older Safari versions. No trimming, gain,
normalization, speed, or pitch changes were applied. Source container durations
include Vorbis padding; the durations above describe the decoded WAV files.

Conversion command, with the source and output filenames substituted:

```sh
ffmpeg -nostdin -hide_banner -loglevel error -i source.ogg -map_metadata -1 \
  -c:a pcm_s16le -fflags +bitexact -flags:a +bitexact output.wav
```

Source archive SHA-256:

```text
f36250766ac5bc378c13708ddf12a23a8e54a3251f8d482c7536e51b5dbafa18
```
