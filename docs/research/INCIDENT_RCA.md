# Incident Investigation and RCA Research

Primary reference: [HolmesGPT](https://github.com/HolmesGPT/holmesgpt), snapshot commit `d46f9850c82956b19147adf86963b7a1d36ac965`. SafeOps borrows investigation discipline and tests, not its Python runtime.

## Investigation rules

- Begin with an explicit investigation plan spanning state, logs, metrics, traces and config changes.
- Feed every Tool Result back into the loop and reassess evidence gaps.
- Symptoms are not automatically root causes; follow upstream causal links.
- Every final claim links to Tool/Observation evidence.
- Direct evidence can support a firm conclusion; indirect correlation remains a candidate.
- A missing requested entity cannot be silently replaced with a similar name.
- Missing permissions/data are investigation limits, never invented evidence.
- Preserve negative findings and independent multiple root causes through compaction/restart.

HolmesGPT's staged prompt and evidence constraints are visible in its [generic investigation prompt](https://github.com/HolmesGPT/holmesgpt/blob/d46f9850c82956b19147adf86963b7a1d36ac965/holmes/plugins/prompts/generic_ask.jinja2), while its [tool loop](https://github.com/HolmesGPT/holmesgpt/blob/d46f9850c82956b19147adf86963b7a1d36ac965/holmes/core/tool_calling_llm.py) and [repeat safeguard](https://github.com/HolmesGPT/holmesgpt/blob/d46f9850c82956b19147adf86963b7a1d36ac965/holmes/core/safeguards.py) inform bounded execution.

## SafeOps domain model

An Investigation advances through scoping, collecting, correlating, testing hypotheses, gathering missing evidence, and confirmed/inconclusive completion. Evidence has polarity (supports/contradicts/rules-out), strength (direct/correlated/indirect), freshness, collection limitations and source reliability. Hypotheses retain support, contradiction, missing evidence and next tests.

Completion is a code gate: targets/time window resolved; conclusions have evidence; contradictions and limits disclosed; result is confirmed/inconclusive/D3; remediation tasks have post-verification evidence. `FINAL` cannot merely mean the model stopped calling tools.

Confidence uses the project deterministic formula, never the model's self-rating. LLM output is limited to candidate hypotheses, explanations and controlled next-step proposals.

## Tests

Use recorded Observation/ToolResult sequences and controlled labs. Cover cascading upstream causes, direct versus indirect evidence, config/metric time correlation, multiple roots, negative findings, permission/data absence, contradictions, order perturbation, restart continuity and model nondeterminism. HolmesGPT fixtures provide case patterns, including [cascading failures](https://github.com/HolmesGPT/holmesgpt/tree/d46f9850c82956b19147adf86963b7a1d36ac965/tests/llm/fixtures/test_ask_holmes/68_cascading_failures) and [overconfidence prevention](https://github.com/HolmesGPT/holmesgpt/tree/d46f9850c82956b19147adf86963b7a1d36ac965/tests/llm/fixtures/test_ask_holmes/249_overconfidence_postgres).

