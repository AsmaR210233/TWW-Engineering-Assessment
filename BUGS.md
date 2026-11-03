# Bugs in debugging/main.go

Verification used for all: `go run -race . events.jsonl | diff - expected_output.txt` is empty,
repeated 20 times, and also under `TZ=Asia/Kolkata`, `America/Los_Angeles`, `Pacific/Auckland`.

## 1. Duplicate event_ids only removed within a batch
- **What:** `seen` was created inside `processBatch`, so a retry landing in a different 200-line batch was counted again.
- **Why:** spec says each event_id counts once across the whole file; the dedup scope was one batch.
- **Fix:** moved `seen` to a package-level map (declared once, deleted the per-call `make`).
- **Verified:** before, cmp_A sent=2025; after, 1987 (expected).

## 2. unique_opens keyed by contact only
- **What:** `openedBy[ev.ContactID]` made a contact count as a unique opener in only one campaign.
- **Why:** spec says distinct per campaign; the same contact opening in two campaigns counts once in each.
- **Fix:** key is `CampaignID + "|" + ContactID`.
- **Verified:** cmp_A unique_opens went 549 -> 942 (expected).

## 3. Daily delivered buckets used `Timestamp.Local()`
- **What:** bucket date came from the machine's timezone.
- **Why:** spec says UTC date; output changed with the host's TZ (and hid on a UTC machine).
- **Fix:** `.Local()` -> `.UTC()`.
- **Verified:** diff empty under three different `TZ` values (before the fix, Asia/Kolkata moved events across days).

## 4. Data race on counters
- **What:** 8 worker goroutines did `cs.Sent++` etc. on the same `*CampaignStats` with no synchronisation.
- **Why:** `++` is read-modify-write; the comment "increments are cheap" is not a safety argument. Lost updates are possible.
- **Fix:** package-level `sync.Mutex` locked in `apply`.
- **Verified:** `go run -race` reported DATA RACE before (main.go `apply`), nothing after; 20 repeated runs match.
