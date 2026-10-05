#!/usr/bin/env bash
set -euo pipefail
ROOT="${1:?directory required}"
[[ "$ROOT" == /tmp/xiaolan-feedback-cherry-* && -d "$ROOT" && "$ROOT" != *..* ]] || exit 2
mkdir -p "$ROOT/ogg"
for name in feedback_greeting_female feedback_greeting_male feedback_greeting_child feedback_greeting_neutral feedback_accepted_female feedback_accepted_male feedback_accepted_child feedback_accepted_neutral feedback_completed feedback_failed feedback_capture_completed feedback_info; do
  [[ -s "$ROOT/$name.wav" ]]
  ffmpeg -v error -nostdin -n -i "$ROOT/$name.wav" -ar 16000 -ac 1 -c:a libopus -b:a 24k -frame_duration 60 "$ROOT/ogg/$name.ogg"
  ffprobe -v error -show_entries stream=codec_name,sample_rate,channels -show_entries format=duration,size -of csv=p=0 "$ROOT/ogg/$name.ogg"
done
(cd "$ROOT/ogg" && sha256sum ./*.ogg)
