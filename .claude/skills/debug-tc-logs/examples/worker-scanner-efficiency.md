# Worker-Scanner Efficiency & Concurrency Debugging

Playbook for measuring worker-scanner performance and *proving* whether a change
(cascade fast-teardown, scanner concurrency, idle-timeout tuning) actually helped.
The scanner processes are `workerscanner-azure` and `workerscanner` (generic GCP/AWS);
the provisioner (`provisioner`) is a **separate process**.

> The #1 mistake: comparing raw scanner **loop duration** before/after. Loop duration is
> dominated by **worker count** (how many workers were scanned), which swings wildly with
> provisioning load, weekday/weekend, and surges. A raw before/after almost always
> measures *that period's load*, not your change. **Always load-normalize.**

---

## 0. The metrics and where they live

| What | Log type | Key fields |
|---|---|---|
| Loop wall-clock | `monitor.periodic` (`name=workerScannerAzure` / `workerScanner` / `provisioner`) | `duration` (ms), `ts`, `status` |
| Workers scanned per loop | `scan-seen` (`providerId=azure2`, `community-tc-workers-azure`, `fxci-level1-gcp`, …) | `total`, `ts` |
| Azure API calls per loop | `cloud-api-metrics` (per `providerId`) | `total` (N calls), `avg`/`p95`/`max` (latency ms), `failed`, `retries`, `byStatus` |
| Rate-limit backoff | `cloud-api-paused`, `cloud-api-resumed` | `duration`, `reason`, `queueName` |
| Azure 429s | `azure-throttled` | `operationType`, `retryAfterSeconds` |
| Fast-teardown fired | `azure-teardown-mode` | (presence = cascade fast-path used) |

**Gotcha:** `cloud-api-metrics` is emitted by BOTH the scanner (`scanCleanup`) and the
provisioner (`cleanup`) for the same `providerId`. To attribute it, filter by
`resource.labels.container_name` (`worker-manager-workerscanner-azure` vs
`worker-manager-provisioner`) — but note tc-logview's `--raw` payload shape is
inconsistent for this type, so it's often easier to read these off **Grafana**.

---

## 1. Is the scanner slow? (triage)

```bash
# Loop durations, recent
tc-logview query -e <env> --type monitor.periodic \
  --filter 'jsonPayload.Fields.name="workerScannerAzure"' --since 6h --limit 200 --json \
  | jq -r '[.ts,(.duration|tonumber/1000|floor)]|@tsv' | sort
```

Then get worker counts to see if slowness is just load:

```bash
tc-logview query -e <env> --type scan-seen \
  --filter 'jsonPayload.Fields.providerId="azure2"' --since 6h --limit 200 --json \
  | jq -r 'select(.total!="0")|[.ts,.total]|@tsv' | sort
```

If loop duration tracks worker count, it's **load-bound**, not a regression.

---

## 2. Load-normalized before/after (the honest comparison)

Pull loop durations and worker counts for a **before** and an **after** window, then join
by timestamp and compare **ms/worker within worker-count bins**. Use `scanner-efficiency.py`
(next to this file):

```bash
# one row per loop; split days into sub-day windows to beat the 500-row cap
for w in "00 11" "12 23"; do set -- $w
  tc-logview query -e fx-ci --type monitor.periodic \
    --filter 'jsonPayload.Fields.name="workerScannerAzure"' \
    --from 2026-07-01T$1:00:00Z --to 2026-07-01T$2:59:59Z --limit 500 --json \
    | jq -r '[.ts,.duration]|@tsv' >> before.per.tsv
  tc-logview query -e fx-ci --type scan-seen \
    --filter 'jsonPayload.Fields.providerId="azure2"' \
    --from 2026-07-01T$1:00:00Z --to 2026-07-01T$2:59:59Z --limit 500 --json \
    | jq -r '[.ts,.total]|@tsv' >> before.seen.tsv
done
# ...repeat for the after window into after.per.tsv / after.seen.tsv...

python3 scanner-efficiency.py \
  --before-per before.per.tsv --before-seen before.seen.tsv \
  --after-per  after.per.tsv  --after-seen  after.seen.tsv
```

Read the **per-bin `ms/worker` speedup** and check `median workers/loop` is similar on
both sides. If load isn't balanced, trust the per-bin rows, not the overall number.

**Isolate ONE change:** to measure concurrency alone, both windows must be cascade-on
(or both cascade-off). To measure cascade alone, both must be concurrency-off. Otherwise
you conflate two changes.

**Worked results (fx-ci, historical):**
- Cascade fast-teardown: ~12% cheaper per worker overall, +15–21% in 300–1000-worker bins.
- Scanner `concurrency=2`: ~2.5× overall (2.0× small loops → 3.5–3.8× on 600+ backlog loops).

---

## 3. Detecting a deploy step-change (when did a change land?)

A deploy shows as a **sharp step + a brief loop-cadence overlap** (rolling restart = old
pod finishing a loop while the new pod starts, so loop records briefly overlap). It is NOT
a gradual drift (that's diurnal load).

```bash
# raw timestamped stream — eyeball the step
tc-logview query -e <env> --type monitor.periodic \
  --filter 'jsonPayload.Fields.name="workerScannerAzure"' --since 12h --limit 500 --json \
  | jq -r '[.ts,(.duration|tonumber/1000|floor)]|@tsv' | sort
```

**Do NOT trust daily/hourly p50 aggregates to find the step** — they average pre- and
post-deploy loops together AND get truncated by the 500-row cap (a busy `workerScanner`
is ~1000 loops/day). Pull the raw stream and look for the instant it drops.

---

## 4. Proving concurrency actually parallelizes (not just "got faster")

Evidence hierarchy, weakest → strongest:

1. **Config**: `WORKER_SCANNER_CONCURRENCY` set. Intent only.
2. **Constant work, halved time**: worker count unchanged across the cutover while loop
   time halves ⇒ throughput doubled ⇒ parallelism. Cleanest proof from standard logs.
   ```bash
   # worker count steady across the step?
   tc-logview query -e <env> --type scan-seen \
     --filter 'jsonPayload.Fields.providerId="azure2"' --from <t-30m> --to <t+30m> --limit 200 --json \
     | jq -r 'select(.total!="0")|[.ts,.total]|@tsv' | sort
   ```
3. **Effective concurrency = (N × L) / T** — dispositive and load-independent. In one loop
   of wall-clock `T` seconds the scanner makes `N` Azure calls averaging `L` seconds each
   (`cloud-api-metrics.total`, `.avg`). Serial ⇒ `N·L ≤ T` (ratio ~1); two-in-flight ⇒
   ratio ~2. You can't spend more API-seconds than wall-seconds without overlap. Best read
   off Grafana (metric ÷ loop duration) because the tc-logview payload is fiddly.

---

## 5. Safety checks before/after raising concurrency

Concurrency raises the scanner's Azure call rate. The `CloudAPI` PQueue caps throughput at
~20 calls/s **per process** (`intervalCap` ~2000/100s), so the local rate is self-limiting.
The real risks are (a) **shared-subscription contention** — scanner + provisioner + web all
draw on one Azure subscription, and the cap is per-process — and (b) more per-worker
timeouts. Check all of these over a **post-deploy-only** window:

```bash
S=<deploy>; E=<now>
# rate-limit backoff and 429s — should stay near baseline (~tens/week)
tc-logview query -e <env> --type cloud-api-paused --from $S --to $E --limit 200 --json | wc -l
tc-logview query -e <env> --type azure-throttled  --from $S --to $E --limit 200 --json | wc -l
# call failures/retries — should be ~0
tc-logview query -e <env> --type cloud-api-metrics \
  --filter 'jsonPayload.Fields.providerId="azure2"' --from $S --to $E --limit 500 --json \
  | jq -r 'select((.failed|tonumber)>0 or (.retries|tonumber)>0)|[.ts,.failed,.retries]|@tsv'
# error mix (grouped)
tc-logview query -e <env> --type monitor.error \
  --filter 'jsonPayload.serviceContext.service="worker-manager"' --from $S --to $E --limit 500 --json \
  | jq -r '.message[:45]' | sed -E 's/[0-9a-f-]{8,}.*//; s/[0-9]+//g' | sort | uniq -c | sort -rn
```

**`checkWorker timed out` is the binding signal for raising concurrency** — not rate
limiting. Each `checkWorker` has a hard 60s `withTimeout` (worker-scanner.js). Timeouts are
caught and non-fatal (the worker is re-checked next loop), but they rise with concurrency:
more parallel checks share the queue, each waits longer, and Azure outlier-latency calls
(occasionally 30–77s) blow the budget. Compare pre/post over identical hours:

```bash
for W in "PRE 2026-07-01T13:05:00Z 2026-07-02T08:00:00Z" "POST 2026-07-02T13:05:00Z 2026-07-03T08:00:00Z"; do
  set -- $W
  echo -n "$1 checkWorker timeouts: "
  tc-logview query -e fx-ci --type monitor.error \
    --filter 'jsonPayload.serviceContext.service="worker-manager"' --from $2 --to $3 --limit 500 --json \
    | jq -r 'select(.message|test("checkWorker timed out"))' | wc -l
done
```

**Concurrency ceiling math:** effective throughput ≈ `C ÷ latency` calls/s. At ~375ms Azure
latency, `C=4` ≈ 11/s (~53% of the 20/s cap), `C≈7` hits the cap. Past that the PQueue paces
it — diminishing returns, not danger. Raising the 60s `checkWorker` timeout does NOT fix the
root (Azure outlier latency + call volume); it only holds in-flight slots longer, costing the
throughput you just gained. Better levers: reduce calls (list-based reconciliation), or a
tighter *per-call* Azure timeout so a hung call fails fast and retries.

---

## 6. Provisioner "loop interference"

`provision loop interference` (and the scanner's `scan loop interference`) fires when a new
loop is scheduled while the previous one is **still running** — an overrun past lib-iterate's
max iteration time (~300s for the provisioner). It's a deliberate terminal error that
restarts the pod to avoid getting stuck. A cluster of them ~10s apart = ONE incident (retries
until restart), not N incidents.

Diagnose the overrun:
```bash
# when did it fire?
tc-logview query -e <env> --type monitor.error \
  --filter 'jsonPayload.serviceContext.service="worker-manager"' --from $S --to $E --limit 500 --json \
  | jq -r 'select(.message|test("loop interference"))|.ts' | sort
# which loop overran? (look for a ~300s provisioner loop just before)
tc-logview query -e <env> --type monitor.periodic \
  --filter 'jsonPayload.Fields.name="provisioner"' --from $S --to $E --limit 500 --json \
  | jq -r '[(.duration|tonumber/1000|floor),.ts]|@tsv' | sort -rn | head
# rate limits vs outlier latency? check byStatus (429s) and max latency
tc-logview query -e <env> --type cloud-api-metrics \
  --filter 'jsonPayload.Fields.providerId="azure2"' --from $S --to $E --limit 200 --json \
  | jq -r '[.ts,"avg="+.avg,"max="+.max,"byStatus="+.byStatus]|@tsv'
```

If `byStatus` is all `200`s (no 429s) but `max` shows 30–77s single calls, the cause is
**Azure ARM outlier latency**, not throttling — a few slow VM-create calls balloon the loop
past 300s. Raising scanner concurrency can *contribute* by adding shared-subscription
contention (it's per-process rate-capped but the subscription is shared), so if `loop
interference` appears only after a concurrency bump, suspect contention and watch it.

---

## Reproduce-safe notes
- `tc-logview` tokens expire ~1h — long investigations need a fresh token mid-way.
- Daily single queries hit the 500-row cap for high-volume streams → split into sub-day windows.
- `awk` in some envs rejects `next` inside an `END` block — use Python (`scanner-efficiency.py`) for percentiles.
