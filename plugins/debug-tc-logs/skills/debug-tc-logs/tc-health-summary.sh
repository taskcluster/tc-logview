#!/usr/bin/env bash
# tc-health-summary.sh — aggregate Taskcluster health signals across environments
# Usage: tc-health-summary.sh [--since 12h] [--envs "community-tc fx-ci"]
# Runs queries in parallel per environment and prints a structured summary.

SINCE="${SINCE:-12h}"
ENVS="${ENVS:-community-tc fx-ci}"
LIMIT=500
TMPDIR_BASE=$(mktemp -d)
trap 'rm -rf "$TMPDIR_BASE"' EXIT

while [[ $# -gt 0 ]]; do
  case $1 in
    --since) SINCE="$2"; shift 2 ;;
    --envs)  ENVS="$2";  shift 2 ;;
    *) echo "Unknown flag: $1"; exit 1 ;;
  esac
done

# Helper: run tc-logview and emit only JSON lines
query() {
  tc-logview query "$@" --json 2>/dev/null || true
}

# Run all queries for a single environment in parallel
query_env() {
  local env="$1"
  local dir="$TMPDIR_BASE/$env"
  mkdir -p "$dir"

  query -e "$env" --type monitor.error --since "$SINCE" --limit "$LIMIT" \
    > "$dir/errors.jsonl" &

  query -e "$env" --type monitor.apiMethod --where 'statusCode="500"' \
    --since "$SINCE" --limit "$LIMIT" \
    > "$dir/api_500.jsonl" &

  query -e "$env" --type monitor.apiMethod --where 'statusCode="403"' \
    --since "$SINCE" --limit "$LIMIT" \
    > "$dir/api_403.jsonl" &

  query -e "$env" --type worker-requested --since "$SINCE" --limit "$LIMIT" \
    > "$dir/worker_requested.jsonl" &

  query -e "$env" --type worker-stopped --since "$SINCE" --limit "$LIMIT" \
    > "$dir/worker_stopped.jsonl" &

  # Claim-expired tasks (task-exception from claim-resolver)
  query -e "$env" --type task-exception \
    --filter 'jsonPayload.Logger="taskcluster.queue.claim-resolver"' \
    --since "$SINCE" --limit "$LIMIT" \
    > "$dir/claim_expired.jsonl" &

  # Deadline-exceeded tasks (task-exception from deadline-resolver)
  query -e "$env" --type task-exception \
    --filter 'jsonPayload.Logger="taskcluster.queue.deadline-resolver"' \
    --since "$SINCE" --limit "$LIMIT" \
    > "$dir/deadline_exceeded.jsonl" &

  # Worker-manager periodic loop failures
  for loop in workerScanner workerScannerAzure provisioner; do
    query -e "$env" --type monitor.periodic \
      --where "name=$loop" --where 'status!=success' \
      --since "$SINCE" --limit "$LIMIT" \
      > "$dir/periodic_${loop}.jsonl" &
  done

  wait
}

count() {
  local f="${1:-}"
  [[ -s "$f" ]] && wc -l < "$f" | tr -d ' ' || echo 0
}

# Top N: jq_expr produces one string key per JSON line; handles multiline values safely
top_n() {
  local file="$1" expr="$2" n="${3:-5}"
  if [[ ! -s "$file" ]]; then echo "  (none)"; return; fi
  jq -r "$expr" "$file" 2>/dev/null \
    | sort | uniq -c | sort -rn | head -"$n" \
    | awk '{cnt=$1; $1=""; sub(/^ /,""); printf "  %4s × %s\n", cnt, $0}' \
    || true
}

summarize_env() {
  local env="$1"
  local dir="$TMPDIR_BASE/$env"

  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  $env  (--since $SINCE)"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  # Errors — group by "service | first-line-of-message"
  local err_total; err_total=$(count "$dir/errors.jsonl")
  echo ""
  echo "ERRORS  ($err_total total)"
  if [[ $err_total -gt 0 ]]; then
    echo "  By type:"
    top_n "$dir/errors.jsonl" \
      '[.Service // "?", ((.message // .name // "?") | split("\n")[0])] | join(" | ")'
    echo "  By hour (UTC):"
    top_n "$dir/errors.jsonl" \
      '(.ts | split("T")[1] | split(":")[0]) + "h"' 24
  fi

  # API 500s
  local n500; n500=$(count "$dir/api_500.jsonl")
  echo ""
  echo "API 500s  ($n500 total)"
  if [[ $n500 -gt 0 ]]; then
    top_n "$dir/api_500.jsonl" '.name // "unknown"'
  fi

  # API 403s
  local n403; n403=$(count "$dir/api_403.jsonl")
  echo ""
  echo "API 403s  ($n403 total)"
  if [[ $n403 -gt 0 ]]; then
    top_n "$dir/api_403.jsonl" '.name // "unknown"'
  fi

  # Claim-expired
  local nce; nce=$(count "$dir/claim_expired.jsonl")
  echo ""
  echo "CLAIM-EXPIRED  ($nce tasks)"
  if [[ $nce -gt 0 ]]; then
    echo "  By task queue:"
    top_n "$dir/claim_expired.jsonl" '.taskQueueId // "unknown"'
  fi

  # Deadline-exceeded
  local nde; nde=$(count "$dir/deadline_exceeded.jsonl")
  echo ""
  echo "DEADLINE-EXCEEDED  ($nde tasks)"
  if [[ $nde -gt 0 ]]; then
    echo "  By task queue:"
    top_n "$dir/deadline_exceeded.jsonl" '.taskQueueId // "unknown"'
  fi

  # Workers requested
  local nwr; nwr=$(count "$dir/worker_requested.jsonl")
  echo ""
  echo "WORKERS REQUESTED  ($nwr total)"
  if [[ $nwr -gt 0 ]]; then
    echo "  By pool (top 8):"
    top_n "$dir/worker_requested.jsonl" '.workerPoolId // "unknown"' 8
    echo "  By provider:"
    top_n "$dir/worker_requested.jsonl" '.providerId // "unknown"'
  fi

  # Worker stopped
  local nws; nws=$(count "$dir/worker_stopped.jsonl")
  echo ""
  echo "WORKERS STOPPED  ($nws total)"
  if [[ $nws -gt 0 ]]; then
    echo "  By pool (top 8):"
    top_n "$dir/worker_stopped.jsonl" '.workerPoolId // "unknown"' 8
    echo "  By reason:"
    top_n "$dir/worker_stopped.jsonl" '.reason // "(no reason)"' 8
  fi

  # Worker-manager periodic loop failures
  echo ""
  echo "PERIODIC LOOP FAILURES"
  local any_periodic_fail=0
  for loop in workerScanner workerScannerAzure provisioner; do
    local f="$dir/periodic_${loop}.jsonl"
    local nfail; nfail=$(count "$f")
    if [[ $nfail -eq 0 ]]; then continue; fi
    any_periodic_fail=1
    echo "  $loop: ${nfail} failures"
    echo "    By status:"
    top_n "$f" '.status // "unknown"'
    echo "    Max duration:"
    local max_dur; max_dur=$(jq -r '.duration // 0' "$f" 2>/dev/null | sort -n | tail -1)
    echo "    ${max_dur}s"
  done
  [[ $any_periodic_fail -eq 0 ]] && echo "  ✓ All loops healthy (no non-success runs)"

  # Limit warnings
  echo ""
  local warned=0
  for f in errors api_500 api_403 claim_expired deadline_exceeded worker_requested worker_stopped \
           periodic_workerScanner periodic_workerScannerAzure periodic_provisioner; do
    local c; c=$(count "$dir/$f.jsonl")
    if [[ "$c" -ge "$LIMIT" ]]; then
      echo "  ⚠  $f hit limit ($LIMIT) — results truncated, tighten filters or reduce --since"
      warned=1
    fi
  done
  [[ $warned -eq 0 ]] && echo "  ✓ No result limits hit"
}

# ── Main ─────────────────────────────────────────────────────────────────────

echo "Querying: $ENVS  (--since $SINCE, limit $LIMIT per query) ..."

for env in $ENVS; do
  query_env "$env" &
done
wait

echo "Done. Summarizing..."

for env in $ENVS; do
  summarize_env "$env"
done

echo ""
