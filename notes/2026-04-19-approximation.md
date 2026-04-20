# ISSN registration approximation — notes

Snapshot used: `issn.tsv` at 2026-02-16, 2,448,542 registered ISSN.
Problem: issn.org portal is paywalled; sitemap is gone. Only the per-ISSN
content-negotiation endpoint still returns data:

    curl -H "Accept: application/ld+json" https://portal.issn.org/resource/ISSN/<NNNN-NNNC>

We want an approximation of "how many new ISSN have been registered since
2026-02-16, and which ones", without scanning the full 10M ISSN space or
hammering the portal.

## 1. Patterns found in the snapshot

The space of valid ISSN is 10^7 (seven free digits, last digit is a check
digit deterministically computed). The registered subset is not spread
uniformly across this space — it is heavily structured.

### 1.1 Two-digit-prefix bands are allocated, not arbitrary

Counts of registered ISSN by first two digits (snapshot 2026-02-16):

    00: 53768  07: 92358  14: 87121  21: 90692  28: 34470
    01: 92398  08: 82309  15: 90329  22: 88968  29: 53197
    02: 91663  09: 90697  16: 90363  23: 90997  30: 80352
    03: 91061  10: 91858  17: 90863  24: 86901  31: 10004
    04: 18980  11: 92564  18: 89931  25: 92329  87:  2873
    05: 19840  12: 97205  19: 92311  26: 94139
    06:     1  13: 88222  20: 88266  27: 91512

Observations:

- Only prefixes 00–31 are active, plus a legacy pocket at 87 (exactly
  2,873 entries, all at 8750/8755/8756).
- 04, 05, 06 are effectively frozen (tiny or empty bands).
- 00, 28, 29, 30, 31 are visibly non-saturated — these are the current
  working frontier.
- Everything ≥ 32 is unused so far.

### 1.2 Most 4-digit blocks are near-saturated

Each 4-digit prefix holds exactly 1000 checksum-valid ISSN (three free
digits × one valid check digit each). Density buckets (populated blocks
only, total 2880):

    >= 900   1845   (nearly full)
    500–899   753
    100–499   261
    <  100     21   (frontier: 3125=10, 3126=1, 3134=45, etc.)

So most of the allocated universe is already "done". All probing energy
should be concentrated on:

- Sparse blocks (density < some threshold, typically < 900).
- Not-yet-allocated blocks at the tail (3135, 3136, …).

### 1.3 Frontier advances ~40–50 new 4-digit blocks/year

Max populated 4-digit prefix over time (ignoring the 87 pocket):

    2020-03: 2720
    2021-03: 2788
    2023-11: 3028
    2026-02: 3134

→ ~5 new 4-digit blocks per month globally.

### 1.4 Within an active block, allocations are near-sequential

Example: diff between the last two snapshots adds these at prefix 3134:

    6200 6219 6227 6235 6243 6251 6278 6286 6294 6316 6340 6359 6391

Each is the next-or-near-next checksum-valid ISSN. The step of ~8 in the
last-3-digit space is the natural gap between consecutive valid ISSN
(only one of ten check digits is valid per triple → roughly 1 valid per
~1 position in the 3-digit space, but national centres skip and cluster).

**Takeaway for prediction**: for each sparse/frontier block, the next
few new registrations will most often be the successors of max(known)
within sub-ranges that are already "open".

## 2. Estimation framework

Define the relevant population:

- **Active zone A**: 4-digit prefixes 0000..3199 (plus legacy 87XX as a
  rounding error). |A| = 3200 × 1000 = 3.2 × 10^6 checksum-valid ISSN.
- **Known set K**: ISSN in our snapshot. |K| = 2,448,542.
- **Unknown pool U = A \ K**: candidates that might have been registered
  either before the snapshot (miss) or after the snapshot (new).
  |U| ≈ 3.2M − 2.45M ≈ 7.5 × 10^5.

For a uniform random sample of n ISSN drawn from U, let k be the number
that probe as registered. Define p = true fraction of U that is
registered. Then k ~ Binomial(n, p), and:

    p̂ = k / n
    N̂ = p̂ · |U|            (point estimate of "missing registrations")

### 2.1 Normal-approximation 95% CI

Valid when n is moderate and p not too close to 0 or 1:

    se(p̂) = sqrt( p̂ (1 − p̂) / n )
    95% CI for p : p̂ ± 1.96 · se(p̂)
    95% CI for N̂ : |U| · (p̂ ± 1.96 · se(p̂))

### 2.2 Wilson score CI (preferred when p is small or n is moderate)

More reliable for rare events:

    z = 1.96
    denom  = 1 + z² / n
    centre = (p̂ + z² / (2n)) / denom
    half   = z · sqrt( p̂(1−p̂)/n + z²/(4n²) ) / denom
    95% CI for p : [centre − half, centre + half]

### 2.3 Sample-size sizing

To achieve absolute margin M on p (at 95% confidence) the worst-case n
(at p = 0.5) is n ≈ (1.96 / M)² / 1:

    M = 0.05 → n ≈ 384        (±0.05 on p, ±38k on N̂ if |U|=750k)
    M = 0.02 → n ≈ 2401       (±0.02 on p, ±15k on N̂)
    M = 0.01 → n ≈ 9604       (±0.01 on p, ±7.5k on N̂)

When p is small (say p̂ ≤ 0.1), replace worst-case n by
n ≈ z² · p(1−p) / M²; for p = 0.1, M = 0.02 → n ≈ 865.

### 2.4 Worked example

Suppose we probe n = 400 randomly drawn unknowns at 3 s/request (≈ 20
minutes real time) and observe k = 40 registered.

    p̂ = 0.100
    se = sqrt(0.1 · 0.9 / 400) = 0.015
    95% CI for p : [0.0706, 0.1294]
    N̂ = 0.1 · 7.5e5 = 75,000
    95% CI for N̂ : [52,950 , 97,050]

That is a useful, cheap estimate of "how many ISSN are registered but
missing from our snapshot". It does not tell us *which* ones — for that
we need either exhaustive sparse-block scans or sequential frontier
probes.

### 2.5 Stratified sampling (cheaper + tighter)

Uniform sampling over all of U is wasteful: most of U sits inside
near-saturated blocks where p is low, while the frontier blocks are
thin but high-hit. Stratify by "block saturation":

    stratum       density range   |Ui|      expected p   probes needed
    saturated     [900, 999]      ≈ 280k    very low     few (sanity)
    partial       [100, 899]      ≈ 400k    moderate     bulk of budget
    frontier      [0, 99]         ≈  20k    high         bulk or all

The stratified estimator is N̂ = Σ_i p̂_i · |Ui|, with variance
Σ_i (|Ui|²/n_i) · p̂_i(1−p̂_i). Neyman allocation (n_i ∝ |Ui|·σ_i)
concentrates probes on the partial/frontier strata.

### 2.6 Known biases

- **Non-uniform registration timing**. Registrations are not i.i.d.
  across U — they cluster within blocks. Simple random sampling still
  gives an unbiased p̂; block-level estimates need per-block sampling.
- **Legacy / "No data available" responses**. A large population of
  ISSN return HTTP 200 with a JSON-LD body that contains `@context`
  and an `identifiedBy` block but no bibliographic metadata (no
  `mainTitle`, `name`, publisher, etc.). The HTML variant of these
  pages shows "No data available". The portal treats them as known
  ISSN, but for our purposes — "does this ISSN carry a registration
  record we could harvest?" — they do not qualify. Example: `0000-2445`
  returns a 449-byte JSON-LD stub; the entire `0000` block we sampled
  is legacy. The prober now distinguishes three outcomes:
    - `registered=true`  — JSON-LD with title/name/Periodical markers
    - `legacy=true`      — stub response or HTML "No data available"
    - otherwise          — 404 or non-portal response
  Estimates for N̂ use only `registered=true` counts. The estimate
  output also reports `legacy_n_hat` for situational awareness.
- **Cache staleness**. A registered ISSN can be un-registered or merged.
  Cache entries are timestamped and carry a `schema_version`; bump the
  constant in the classifier and run `-mode reclassify` to re-score
  every cached response from the saved JSON-LD body without any network
  traffic.

## 3. Operational rules (portal is fragile and paywalled)

- Serial probing, no concurrency by default.
- Minimum request interval: 2–3 seconds (configurable).
- Exponential backoff on 429 / 5xx, honouring `Retry-After`.
- Hard cap on probes per run (`-limit`).
- Persistent cache keyed by ISSN; negative (404) and positive (200)
  results are both cached so re-runs are cheap.
- Save the JSON-LD body for registered hits so later we can enrich
  without re-fetching.
- User-Agent identifies the project; if the portal operators want us
  to stop, they can see where it comes from.

## 4. Suggested workflow

1. `issnprobe -mode estimate -n 400 -prefix-min 0000 -prefix-max 3199`
   → gives a point estimate and CI for how many ISSN are missing.
2. `issnprobe -mode frontier -prefix-min 3100 -prefix-max 3134`
   → enumerates next-after-max candidates in sparse blocks; high-yield.
3. `issnprobe -mode sparse -sparse-max 50 -prefix-min 3100 -prefix-max 3199`
   → exhaustive scan of truly thin blocks; still bounded (~20k candidates).
4. Feed hits back into the master list and repeat periodically.

With a 3 s polite delay, budgets look like:

    400 probes   ≈ 20 min
    10,000       ≈ 8.3 h
    100,000      ≈ 3.5 days

An estimate inside ±2% is achievable in a single workday of probing.
