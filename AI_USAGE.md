# AI usage

**Tools:** Claude (chat).

**Used for:** planning the order of work; drafting the Go service, tests, and the first pass of the bug fixes; drafting NOTES/BUGS text.

**Suggestion I rejected or changed:** TODO (Sandy: fill in honestly. Example candidates: the AI suggested SQLite via a third-party driver; I kept in-memory/stdlib because ___.)

**One thing AI helped me understand:** TODO (e.g. why returning 4xx for a partly bad batch makes providers retry the good events too).

**AI output I had to debug:** TODO (e.g. a test of mine expected 3 rejections but there were 4; the test was wrong, not the code).

I should be able to explain every line before the interview — read each file once and be ready for: dedup identity, why 200 on mixed batches, the mutex, and each of the four bugs.
