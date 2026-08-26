# Published benchmark reports

Ad-hoc runs written by `bench.sh` land in `results/` and are gitignored — they
accumulate on every invocation. The reports here are the ones cited as evidence
by a card, an ADR, or the project README, so they are tracked.

| File | What it is |
|------|------------|
| `granite4.1-8b-post-l3kf-fix.md` | granite4.1:8b, 14 questions, repeat 1. First run after the `hdf_inspect` segfault fix (hdf-libs-l3kf). HDF arm 0/12 → 7/13. |
| `gpt-oss-20b-post-l3kf-fix.md` | gpt-oss:20b, same. Confirms the fix is not model-specific: HDF arm 0/11 → 8/13. |
| `two-model-k3-study.md` | **The study** (hdf-libs-uqhe.5): granite4.1:8b and gpt-oss:20b, 14 questions, `-repeat 3`, `-adhoc`, temperature 0, num-ctx 32768. Both cost views, 39 scored trials per arm per model. |

The two `post-l3kf-fix` reports are `-repeat 1` and therefore directional. They exist to evidence that the
HDF arm was previously measuring a crashed server (hdf-libs-uqhe.15), not to
settle the HDF-vs-raw question.

Reproduce any of them with:

```bash
export HDF_BIN=/path/to/hdf
./bench.sh <model>
```

## Known limitation of `two-model-k3-study.md`

It was generated before the renderer showed per-question token spread, so its
token columns are means with the variance not displayed (the underlying stddev
is computed, and JSON output carries it as `tokenStdDev`). Later reports show
`mean±stddev` per question. Re-running purely to redraw that column was not
worth two hours of local inference.
