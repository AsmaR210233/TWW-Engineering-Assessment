# NOTES

## Part 1 — Reading the brief

**My interpretation.** Providers POST lifecycle events (sent/delivered/opened/clicked) for messages. I must ingest them idempotently and serve per-campaign numbers for a live dashboard. Correct counts despite retries and out-of-order/late arrival matter more than throughput today.

**Assumptions**
- `event_id` is globally unique per provider event (not only per campaign).
- Providers retry on non-2xx, so a permanently bad event must not make the request fail.
- Dashboard wants both "how many events" and "how many people", and ratios.
- Stats are cumulative for the campaign (no date range asked for).
- Losing data on restart is acceptable for this exercise (in-memory), but I'd flag it.
- Contacts are identified by `contact_id`; one message per contact per campaign is NOT assumed.

**Ambiguities noticed**
- What is "the same" event: event_id only? event_id + content?
- "Opened": total open events or distinct contacts? Same question for clicked.
- Should opened without delivered/sent be counted (out of order)? Should delivered > sent be possible?
- Stats by event time or arrival time? Any time window/timezone for the dashboard?
- Unknown event types (e.g. `spam_report`, `bounced`), missing fields, bad timestamps: reject or store?
- Does a campaign that exists but has no events yet return zeros or 404?
- Durability, retention, and who authenticates providers.

**Questions for the PM**
1. Should "opened" show unique people or raw opens (or both)?
2. Are there other event types coming (bounce, unsubscribe, spam)?
3. How fresh must the dashboard be (seconds vs minutes)?
4. Is restart data loss acceptable? Retention period?
5. Do providers ever correct an event (same id, new content)? Which one wins?
6. Per-day/hourly breakdowns needed?

**Priorities** (in order): 1) correct, idempotent ingest with per-event validation; 2) stats with clear definitions; 3) concurrency safety + tests on those; 4) debugging task; 5) memos; 6) optional events list (done, simple).

## Decisions made in the service
- **Identity:** `event_id`. Same id + same content (campaign, contact, type, instant) = duplicate, ignored. Same id + different content = **conflict**: first write kept, reported in response, never counted twice. (Seed has 3 such cases.)
- **POST /events:** body must be a JSON array (else 400; >1000 events or >5MB else 413). Each element is decoded separately (`json.RawMessage`), so one bad event doesn't lose the other 199. Returns **200** with `{received, accepted, duplicates, conflicts, rejected:[{index,event_id,reason}]}`. Why 200: a 4xx/5xx makes the provider retry the whole batch forever.
- **Reject:** non-object/mistyped elements, empty/missing event_id/campaign_id/contact_id, unknown type (e.g. `spam_report`), timestamp not RFC 3339.
- **Normalise:** type lower-cased/trimmed (`OPENED` -> `opened`), IDs trimmed, timestamps converted to UTC.
- **Stats:** `total_events` per type (distinct events after dedup) and `unique_contacts` per type; `rates` computed on unique contacts (null when denominator is 0); first/last event time; `as_of`. Opened before delivered is counted — arrival order doesn't matter. Unknown campaign = 404 (can't tell a typo from an empty campaign).
- **Storage:** in-memory map by event_id + per-campaign counters/contact sets updated at ingest, so a stats read is O(1)-ish. Chosen for zero dependencies (no module proxy in my sandbox) and simplicity; not durable.
- **Concurrency:** one `sync.RWMutex`; dedup check and counter update are one critical section. Tested with 50 goroutines and `-race`.
- **Not validated:** timestamps far in the future/past, campaign existence, metadata shape.

## Part 4 — Scale memo (100M events/day ≈ 1,150/s average, maybe 10x peaks)
- **Breaks first:** memory (every event and every contact set kept in RAM, lost on restart), and the single global lock/process. A restart also loses all state, which is unacceptable at that value.
- **Change, in order:** (1) durable store with a unique key on event_id (Postgres first; consider ClickHouse later for analytics); (2) keep the dedup + write atomic via `INSERT ... ON CONFLICT DO NOTHING`; (3) batch writes; (4) pre-aggregated counters per campaign (per hour) updated from accepted inserts, rather than scanning; (5) partition by campaign/time; (6) queue if bursts or DB latency hurt.
- **API contract:** the webhook and stats shapes stay. Stats may become slightly eventually consistent. Storage design does change.
- **Queue?** Probably yes, to absorb bursts and ack providers fast. New problems: at-least-once delivery, consumer lag, ordering across partitions, poison messages, dashboards behind reality.
- **New duplicate sources:** consumer redelivery, a crash between DB write and offset commit, counter increments applied twice, replays/backfills. Dedup must stay in the durable write path, not in memory.
- **Trustworthy counters:** raw events are source of truth; counters derived from rows that were actually inserted; periodic reconciliation (recount vs counters) and alert on drift; replay is safe because of idempotency.
- **Monitor:** ingest rate, p99 latency, 4xx/5xx, reject/duplicate/conflict rates, queue lag, DB write latency, event-time vs arrival-time lag, counter vs recount drift.
- **Deliberately not solving yet:** exactly-once end to end, multi-region, unique-contact counts at huge cardinality (would use approximate or per-bucket sets), retention/archival, per-provider auth/rate limits.

## Part 5 — The angry marketer
Sent flat at 1,000,000; Delivered 970,000 -> 975,000; Opened 250,000 -> 248,000; Clicked unchanged.

**Plausible explanations**
- Delivered up: late/out-of-order delivered events arriving (completely expected), provider backfill after an outage, retries finally succeeding.
- Opened down, expected-behaviour causes: metric is *unique* contacts and the definition/dedup changed; bot/proxy-open filtering (e.g. mail privacy prefetch) applied retroactively; provider corrected or revoked earlier opens; events reclassified or a type mapping changed.
- Opened down, system causes (bugs): conflicting event_id rewrites replaced earlier events; a deploy changed validation and rejected some opens now; a restart lost in-memory data and partial replay; replica/cache lag or two dashboard nodes with different data; counter vs recount drift; different time window/timezone between the two loads; a bad backfill/reprocessing job.
- Display: stale cache at 10:00 vs fresh at 10:30, rounding, or different campaigns grouped.

**Is rising Delivered suspicious?** No, on its own. Late delivered is normal, and it's still <= Sent. It would be suspicious only if it exceeded Sent or the increase wasn't backed by new event rows.

**Check first:** compare raw event rows for the campaign at both times (count by type, by event_id, with arrival timestamps), and confirm both numbers use the same definition. In an append-only, deduped system, a count of distinct opens can't go down, so a drop means either the definition/filter changed or rows were removed/rewritten. If I can account for the 2,000 difference as filtered/corrected opens, it's expected behaviour and needs a better explanation on the dashboard; if rows disappeared or the same query gives different answers, it's a bug.

## What I completed / what I intentionally skipped
- Completed: ingest with per-event validation and dedup, stats, optional events list, tests (dedup, mixed batch, out-of-order, conflict, concurrency, seed), all four debugging fixes, memos.
- Skipped: durable storage (needs a dependency; stated trade-off), per-day breakdowns, event-time windowing, auth, cursor pagination (list uses offset, which is unstable when late events arrive), future-timestamp checks, metrics/logging.

## If I had another day
- SQLite/Postgres with `event_id` primary key; transactional counters.
- Cursor pagination; hourly/daily stats using event time; bot-open handling.
- Structured logs and metrics for reject/duplicate/conflict rates; benchmark ingest.
