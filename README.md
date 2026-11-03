# Campaign Events service

Go 1.22+, stdlib only, in-memory storage (data is lost on restart — see NOTES.md).

## Run
    go run .            # listens on :8080
    go test -race ./... # tests (incl. concurrency + seed file)

## Endpoints
    # ingest (JSON array; 200 + per-event report, 400 if body isn't an array)
    curl -s -X POST localhost:8080/events --data-binary @seed/events.json

    # dashboard stats
    curl -s localhost:8080/campaigns/cmp_summer_sale/stats

    # recent activity (newest first; ?limit=1..500 &offset=)
    curl -s "localhost:8080/campaigns/cmp_summer_sale/events?limit=20"

Debugging exercise: `cd debugging && go run -race . events.jsonl | diff - expected_output.txt`
