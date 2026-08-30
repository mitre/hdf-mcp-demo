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

## What the raw-file arm can do

The raw arm is the study's denominator, so its capability determines what every
comparison means. It has four tools: read a whole (small) file, search by
substring **or regular expression** reporting the total **match** count with a
window around each hit, select a value from JSON by dot path, and page through
lines. Match counting matters because most real scan files are minified — every
finding sits on one line, and counting matching *lines* answers 1 where the true
count is hundreds.

It deliberately does **not** have a shell. That keeps runs reproducible and keeps
the arm from degenerating into "read the whole file", which would rebuild the
whole-file strawman these measurements exist to avoid. A real agent in a CI
container would have `grep` and `jq`; this arm approximates them, and is
therefore a floor for what a competent raw-file agent achieves, not a ceiling.

Tool responses are kept deliberately small. A response is re-sent on every later
turn, so verbosity compounds: an earlier, more generous version of these same
tools raised the arm's token cost 74% and made it hit the iteration cap more
often. Richer tools made the agent worse until the responses were trimmed.

## The two cost views

| view | what it assumes |
|------|-----------------|
| **pipeline** (amortized) | Conversion already happened out of band, so a query costs roughly nothing extra. This is the steady-state case for a pipeline that normalizes on ingest. |
| **ad-hoc** (conversion-included) | The agent converts on demand inside the measurement, paying the `hdf_convert` round-trip. This is the one-shot "here is a scan, answer this" case. |

Both are reported because both are real workflows, and they can disagree
substantially.

## The cost multiplier

`mult` is `hdfTok / rawTok` for one question — above 1 means the HDF arm cost
more. It is shown **only when both arms produced an answer**, and prints `—`
otherwise.

That restriction is not fussiness. A ratio between an arm that answered and one
that gave up measures nothing, and it is not conservatively wrong in a known
direction: a failing arm burns its whole iteration budget, so failures are
*expensive*. In the two-model study, arms averaged 4,552 tokens when correct and
7,955 when failed. Ratios across mismatched outcomes therefore flatter whichever
side happened to quit.

## Bookends: ceiling and oracle

**Oracle** is a term borrowed from testing, where it means a source of
known-correct answers. Here it is the *ideal agent*: a tool call sequence written
by hand in advance that answers the question perfectly on the first try, with no
exploration and no wrong turns. The `oracle` column is the measured token size of
what those calls actually returned when executed against the live server — not an
estimate. It is a **cost** baseline only, and never influences whether an answer
is graded correct; ground truth is computed separately from the document bytes.

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
