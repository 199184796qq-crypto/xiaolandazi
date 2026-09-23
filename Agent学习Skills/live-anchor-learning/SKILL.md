---
name: live-anchor-learning
description: Learn and continuously refine livestream commerce anchor behavior from user-provided high-quality speech samples, user scores, direct feedback, generated speech traces, and verified product facts. Use when the user says to learn a sample, improve the anchor/Control prompt, preserve a style preference, diagnose why speech became robotic or link-heavy, compare a new output against prior good examples, turn repeated feedback into reusable rules, or regression-test live-selling speech without changing unrelated playback/TTS behavior.
---

# Live Anchor Learning

## Goal

Build a stable, reusable learning loop for a livestream commerce anchor agent. Convert examples and user judgments into durable style rules while keeping product facts, compliance constraints, runtime behavior, and style learning separate.

## Core principle

Treat user judgment as the quality target. Learn the **way of speaking** from samples; never promote sample-specific product claims into reusable style rules.

Maintain four separate layers:

1. **Style rules** — how the anchor speaks and structures a continuous product story.
2. **Verified product facts** — what the current product is allowed to claim.
3. **Compliance rules** — what language must be softened, avoided, or fact-checked.
4. **Runtime rules** — TTS, chunking, buffering, interruption, resume, and playback behavior.

Never fix a style problem by casually changing runtime behavior that the user already approved.

## Learning workflow

When new evidence arrives, follow this sequence.

### 1. Classify the evidence

Classify each new item as one or more of:

- **gold sample**: a user-provided speech sample whose style should be learned;
- **generated trace**: actual current agent output;
- **score**: numeric user judgment such as 60/100 or 75/100;
- **direct rule**: explicit user instruction such as “allow mild inference, avoid absolute claims”;
- **negative example**: a phrase or behavior the user dislikes;
- **runtime observation**: playback gap, TTS stop, chunk skip, interruption problem;
- **product fact**: current-product truth that belongs in the fact layer, not the style layer.

### 2. Separate style from facts

For every gold sample, extract only reusable speech mechanics:

- opening pattern;
- continuity and topic depth;
- fact ordering;
- transition style;
- repetition strategy;
- natural spoken fillers;
- product-story density;
- how and when CTA appears;
- how questions are answered and the mainline is resumed;
- closing behavior.

Do not copy product-specific claims from a sample unless they are independently present in the current product fact source.

### 3. Compare with the latest generated trace

Compare the sample and current output on the dimensions in `references/scoring-rubric.md`.

Identify the smallest set of causes for the gap. Prefer root causes such as:

- forced stage structure makes the model switch topics;
- not enough verified product material;
- link/parameter language dominates;
- prompt contains conflicting instructions;
- fact guard is deleting too much and turning speech into a summary;
- compliance guard is too weak or too aggressive;
- runtime dropped later chunks.

Do not solve symptoms by adding random prompt text.

### 4. Decide what can be promoted into a rule

Use `references/learning-policy.md`.

Promote an explicit user instruction immediately unless it conflicts with safety, truth, or a newer explicit instruction.

Treat one good sample as a candidate pattern, not a universal rule. Promote recurring patterns only after repeated evidence or strong user confirmation.

### 5. Update the learning records

Keep these files current:

- `references/prompt-kernel.md` — distilled stable anchor behavior;
- `references/sample-library.md` — representative positive and negative examples;
- `references/learning-log.md` — human-readable decisions and score changes;
- `references/learning-ledger.jsonl` — append-only machine-readable evidence.

For deterministic ledger updates, use `scripts/record_learning.py`.

Never overwrite the original wording of a user sample. Add interpretation beside it.

### 6. Apply changes narrowly

When modifying the live system:

- read the current prompt and runtime flow first;
- patch the smallest responsible layer;
- preserve approved TTS/playback behavior unless the evidence is runtime-related;
- do not mix sample facts into product facts;
- keep weak inference separate from hard factual claims;
- prefer one-pass continuous product-story generation, then semantic playback chunking, when the user wants one coherent speech.

See `references/project-integration.md` for the current local POC integration notes.

### 7. Regression-test before declaring improvement

At minimum:

1. Generate one full uninterrupted speech using the current verified facts.
2. Check factual leakage from the reference sample.
3. Check that later portions do not collapse into parameter/link recital.
4. Check that the speech remains one topic even if audio is split into several playback chunks.
5. Check absolute/high-risk advertising language.
6. Compare the output against the previous user score and explain the likely delta.

If testing playback, verify that every expected TTS chunk was actually synthesized and queued; do not infer success from the first few chunks only.

## Evidence hierarchy

Use this order when evidence conflicts:

1. Latest explicit user instruction.
2. Latest user score plus concrete feedback.
3. Repeated patterns across multiple high-scoring samples.
4. One high-quality sample.
5. Model-generated interpretation.

Never infer that a new sample replaces earlier rules unless the user says so or the evidence clearly conflicts and the latest instruction resolves it.

## Output behavior when learning

When the user gives a sample or score, respond with a compact learning update:

- what changed in the learned model;
- what is now a stable rule versus a tentative pattern;
- what should be tested next.

Do not dump the entire internal skill unless asked.

When the user asks for a prompt, provide the complete latest prompt kernel, not only the delta.

## References

Read only what is needed:

- Style promotion and anti-overfitting: `references/learning-policy.md`
- 100-point quality rubric: `references/scoring-rubric.md`
- Current distilled prompt: `references/prompt-kernel.md`
- Sample archive: `references/sample-library.md`
- Compliance and inference boundaries: `references/compliance-boundaries.md`
- Current POC integration notes: `references/project-integration.md`
- Decision history: `references/learning-log.md`
