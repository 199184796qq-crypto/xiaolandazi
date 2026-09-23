# Learning policy

## Promotion levels

Use four levels for learned behavior.

### L0 — raw evidence
Original sample, score, trace, or comment. Preserve verbatim where practical.

### L1 — candidate pattern
A plausible style pattern seen in one sample or one generated comparison. Do not make it a hard default yet.

### L2 — stable preference
Promote when one of these is true:

- the user explicitly states it as a preference/rule;
- the same pattern appears in at least two independent high-quality examples and does not conflict with direct feedback;
- a regression test repeatedly improves user score or removes a repeatedly reported defect.

### L3 — hard guardrail
Use for truthfulness, user-explicit prohibitions, compliance, and runtime invariants already approved by the user.

## Anti-overfitting rules

- Never turn one sample's product facts into universal style rules.
- Never copy one sample's exact catchphrases as a mandatory opening.
- Learn structure and rhythm, not wording.
- Keep multiple valid openings/transitions so the agent does not become repetitive.
- A user score without explanation is evidence of overall quality, not proof that every individual phrase was good.
- When a new score improves after several simultaneous changes, record the full change set and avoid claiming one change alone caused the improvement.

## User score interpretation

Treat user scores as calibration anchors:

- <60: major structural mismatch;
- 60–69: direction is right but obvious robotic/structural failures remain;
- 70–79: useful and fairly natural, but consistency, depth, or compliance still needs work;
- 80–89: strong production candidate; focus on polish and repeatability;
- 90+: gold-standard candidate; extract reusable patterns and add to the sample library.

These bands are operational heuristics, not objective truth. The user's direct explanation always overrides them.

## Rule conflict handling

When a new rule conflicts with an older one:

1. Prefer the newer explicit user instruction.
2. Record the superseded rule in the learning log instead of deleting history.
3. Update the prompt kernel to contain only the active rule.
4. Run one regression test specifically targeting the conflict.
