# Current local POC integration

Use these notes only when working on the current `直播伴播` local project. Re-read current files before editing because paths and implementation can change.

## Known POC files

- Source server: `E:\直播伴播\系统设计\直播伴播_POC\director_cycle_test_server.py`
- Source page: `E:\直播伴播\系统设计\直播伴播_POC\director_cycle_test.html`
- Runtime server mirror: `E:\webcodex\director_cycle_test_server.py`
- Runtime page mirror: `E:\webcodex\director_cycle_test.html`
- Local test page: `http://127.0.0.1:8767`

## Current architecture preference

For uninterrupted anchor tests:

1. Generate one complete continuous product-story text.
2. Apply factual/compliance checking without turning it into a dry summary.
3. Split the finished text into semantic playback chunks only after generation.
4. Pre-synthesize with a rolling buffer instead of creating all TTS requests at once.
5. The model should not know internal playback chunk numbers.

## Change discipline

When the user is evaluating speech quality, do not simultaneously refactor unrelated business logic.

If the user says playback is already smooth, freeze TTS/chunk/playback behavior while tuning the anchor prompt unless a new runtime defect is directly observed.
