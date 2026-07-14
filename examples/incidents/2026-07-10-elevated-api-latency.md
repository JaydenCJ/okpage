---
title: Elevated API latency
date: 2026-07-10T14:30:00Z
status: resolved
affected: [API, Website]
---

Between 14:10 and 14:50 UTC, API responses were slower than usual
(p95 above 2 seconds). No requests were dropped.

## Root cause

A nightly backup job was rescheduled and ran during peak hours,
saturating disk IO on the primary database host.

## Timeline

1. 14:10 — latency alert fires, `okpage check` marks API degraded
2. 14:25 — backup job identified as the cause
3. 14:31 — job paused, latency recovering
4. 14:50 — p95 back under 200 ms, incident resolved

## Follow-up

- Pin the backup window in the scheduler config
- Add an IO-pressure check before heavy jobs start
