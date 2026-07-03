#!/usr/bin/env python3
"""
scanner-efficiency.py — load-normalized before/after analysis of worker-scanner loops.

Worker-scanner loop DURATION is dominated by how many workers it scans (worker count),
which swings with provisioning load. So a raw before/after of loop duration is almost
always confounded (by load, weekday/weekend, surges). The honest metric is COST PER
WORKER (ms/worker), compared WITHIN worker-count bins so both sides are at the same load.

This script joins two log streams by timestamp and reports ms/worker per load bin:
  - loop durations : monitor.periodic  name=workerScannerAzure   (fields: ts, duration[ms])
  - worker counts  : scan-seen         providerId=azure2          (fields: ts, total)

Produce the TSVs with tc-logview (one row per loop; split days into sub-windows to beat
the 500-row cap), e.g.:

  tc-logview query -e fx-ci --type monitor.periodic \
    --filter 'jsonPayload.Fields.name="workerScannerAzure"' \
    --from 2026-07-01T00:00:00Z --to 2026-07-01T11:59:59Z --limit 500 --json \
    | jq -r '[.ts,.duration]|@tsv' >> before.per.tsv

  tc-logview query -e fx-ci --type scan-seen \
    --filter 'jsonPayload.Fields.providerId="azure2"' \
    --from 2026-07-01T00:00:00Z --to 2026-07-01T11:59:59Z --limit 500 --json \
    | jq -r '[.ts,.total]|@tsv' >> before.seen.tsv

Then:
  python3 scanner-efficiency.py --before-per before.per.tsv --before-seen before.seen.tsv \
                                --after-per  after.per.tsv  --after-seen  after.seen.tsv

Optional --cut ISO8601 splits a single stream at a deploy boundary: rows in the *-before
files with ts >= cut are dropped, rows in *-after files with ts < cut are dropped. Handy
when before and after live in the same day's files (e.g. a mid-day deploy).

Stdlib only (no deps). Duration is milliseconds; ms/worker = duration_ms / worker_count.
"""
import argparse, glob, statistics as st
from datetime import datetime

def epoch(ts): return datetime.strptime(ts, "%Y-%m-%dT%H:%M:%SZ").timestamp()

def load(patterns, cut=None, keep_after=True):
    rows = []
    for pat in patterns:
        for path in glob.glob(pat):
            for ln in open(path):
                ln = ln.rstrip("\n")
                if not ln: continue
                ts, val = ln.split("\t")
                if cut is not None:
                    if keep_after and ts < cut: continue
                    if not keep_after and ts >= cut: continue
                rows.append((epoch(ts), float(val)))
    return rows

def pairs(per_rows, seen_rows, tol=180):
    """For each scan-seen (worker count) row, find the nearest loop-duration row within
    tol seconds. Returns (workers, dur_seconds, ms_per_worker)."""
    per = sorted(per_rows)
    out = []
    for st_, w in seen_rows:
        if w <= 0: continue
        best, bd = None, 1e18
        for pt, d in per:
            dt = abs(pt - st_)
            if dt < bd: bd, best = dt, d
        if best is not None and bd <= tol:
            out.append((int(w), best / 1000.0, best / w))  # dur is ms -> ms/worker = ms/w
    return out

def med(xs): return st.median(xs) if xs else float("nan")

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--before-per", nargs="+", required=True)
    ap.add_argument("--before-seen", nargs="+", required=True)
    ap.add_argument("--after-per", nargs="+", required=True)
    ap.add_argument("--after-seen", nargs="+", required=True)
    ap.add_argument("--cut", default=None, help="ISO8601 deploy boundary (optional)")
    ap.add_argument("--tol", type=int, default=180, help="join tolerance seconds")
    a = ap.parse_args()

    before = pairs(load(a.before_per, a.cut, keep_after=False),
                   load(a.before_seen, a.cut, keep_after=False), a.tol)
    after = pairs(load(a.after_per, a.cut, keep_after=True),
                  load(a.after_seen, a.cut, keep_after=True), a.tol)

    bins = [(1, 100), (100, 300), (300, 600), (600, 1000), (1000, 10**9)]
    print(f"{'bin':<12}{'n_before':>9}{'ms/wkr_b':>10}{'n_after':>9}{'ms/wkr_a':>10}{'speedup':>9}")
    for lo, hi in bins:
        b = [p for p in before if lo <= p[0] < hi]
        n = [p for p in after if lo <= p[0] < hi]
        mb, mn = med([p[2] for p in b]), med([p[2] for p in n])
        sp = f"{mb/mn:.2f}x" if (b and n) else "-"
        label = f"{lo}-{hi if hi < 10**9 else '+'}"
        print(f"{label:<12}{len(b):>9}{mb:>10.0f}{len(n):>9}{mn:>10.0f}{sp:>9}")
    mb, mn = med([p[2] for p in before]), med([p[2] for p in after])
    print()
    print(f"OVERALL ms/worker  before={mb:.0f} (n={len(before)})  after={mn:.0f} (n={len(after)})  "
          f"speedup={mb/mn:.2f}x" if before and after else "OVERALL: insufficient data")
    print(f"median workers/loop  before={med([p[0] for p in before]):.0f}  "
          f"after={med([p[0] for p in after]):.0f}   "
          f"(if these differ a lot, load is NOT balanced — trust the per-bin rows, not overall)")

if __name__ == "__main__":
    main()
