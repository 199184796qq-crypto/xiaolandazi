# Content-only protocol and accumulated direction-pool prototype

This is an explicit paid **offline, text-only** probe. It reads configured model
roles, calls planning and speech text endpoints, and archives results to a file.
It does not write formal plans, queues, sessions, review decisions or TTS.

The caller supplies a frozen binding separately from model-visible content.
Requests and child item IDs are assigned by the program. Numbered lists are
presentation only; gaps, duplicates, changed numbering and unstructured text
cannot overwrite the caller's task or source relations. Source metadata traces
the input context, not a model-selected citation for each generated statement.

The pool stores every generation. A new generation supplements valid older
directions. Selection prefers directions used less in this scene, then less
across scenes. Different input/rule/model fingerprints preserve older history
without selecting stale generations. Playback acknowledgements are idempotent;
new scenes start usage at zero while pools remain reusable.

The four-visit test calls planning on visits 1 and 4, and speech on all four
visits. Usage confirmations in this CLI are **simulated**; production integration
must consume actual output acknowledgements and must not count staging.
Persistent JSON and restore behavior are covered by regression tests. The CLI
itself starts a fresh offline scene and does not load production pool assets.

Set `ANCHOR_STYLE_PROMPT_DIR` to the complete current template directory. Pass
`-live -input INPUT.json -output RESULTS.json`. The input explicitly contains
probe templates plus content packets and non-model-visible program bindings.
All raw returns, effective writer messages and token usage are archived. A
single preparation/transport failure is recorded without terminating other
cases; successful planning is saved before speech begins.

This prototype proves association and cache mechanics. It does not establish
P0 clearance, factual accuracy, future variation rates or production readiness.
