# Interpreting benchmark results

What the numbers in a `cmd/benchmark` report mean, and what they do not show.
Reports link here rather than restating it, so there is one copy to keep correct.

## Question types are a grading distinction, not a ranking

A question's type says **which arm is entitled to answer it**, not which tool is
better.

| type | meaning |
|------|---------|
| **objective** | One answer both views should reach. A fair apples-to-apples fact. |
| **interpretive** | The fair answer depends on the question's intent, so it is graded bidirectionally — "distinct rule violations" and "raw finding volume" are different correct answers to differently-worded questions over the same scan. |
| **hdf-only** | The raw scanner cannot natively express it (a compliance pass rate, an effective status). The raw arm is out of remit. |
| **raw-only** | The HDF view lacks the fact — for example an SBOM inventory carried as a reference rather than embedded. The HDF arm is out of remit. |

Accuracy is scored **only** over questions an arm can answer. An out-of-remit arm
is reported separately as hallucinated-vs-abstained, never as a failure: inventing
an answer to a question you cannot see is a different mistake from getting it
wrong.

## How answers are graded

Grading parses an `ANSWER: <value>` line and is closed-form — exact and set
matching, **no LLM judge**. A correct answer buried in prose without that line may
be recorded as abstained. That bias is deliberate: prefer a false abstention to
false credit, because a benchmark that flatters itself is worth nothing.

A `failed` arm errored, timed out, or exhausted its iteration budget before
answering. The raw arm works with grep and paginated reads and **no format
hints**, so it must discover each scanner's schema itself — that discovery cost
is part of what is being measured.

## The two cost views

| view | what it assumes |
|------|-----------------|
| **pipeline** (amortized) | Conversion already happened out of band, so a query costs roughly nothing extra. This is the steady-state case for a pipeline that normalizes on ingest. |
| **ad-hoc** (conversion-included) | The agent converts on demand inside the measurement, paying the `hdf_convert` round-trip. This is the one-shot "here is a scan, answer this" case. |

Both are reported because both are real workflows, and they can disagree
substantially.

## Bookends: ceiling and oracle

`rawCeil` is the whole raw file the question spans — a **pessimistic** raw
baseline, since a real agent greps rather than reading everything. `oracle` is
the hand-optimal HDF call's actual response — an **optimistic** HDF baseline. Any
real run sits between them.

`vsOracle` above 1 is expected even for perfect play: a real arm pays the
tool-schema tax and multi-turn accumulation that a single oracle call excludes.
That gap is part of the measurement, not noise.

Some questions show `oracle unreachable`. Those are facts the bounded read
surface cannot return at any price — printed rather than omitted, because a table
showing only the favourable rows would be the steelman objection made real. See
[the HDF MCP guide](https://github.com/mitre/hdf-libs) on the payload boundary.

Bookends are counted with the O200k encoding, while real usage comes from each
model's own tokenizer, so bookend **ratios are approximate** — good for sizing,
not for precise claims.

## What these numbers do not settle

- **Small scans can make HDF cost more.** That is expected and is the point of
  measuring rather than assuming: a bounded tool response still costs a round
  trip, and on a 4KB file there is little to bound.
- **N is small.** A single run at `-repeat 1` is directional. Use `-repeat 3` or
  more before quoting a figure, and read the per-question `mean±stddev` spread.
- **One machine, local models.** Results reflect the models actually run, not
  models in general. Every report names its models, and — for local runs — ships
  an AI-BOM and HDF System document identifying the exact weights.
