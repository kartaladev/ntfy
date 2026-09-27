# Measurements: scale-email-claim-query

The evidence for this change. `.claude/rules/performance-benchmark.md` requires it.

## Thresholds, stated before measuring

These come from `design.md` D3. The two relative ones are what
`TestMeasureClaim` (`sqlstore/email_claim_measure_test.go`) asserts. They hold
on any machine.

1. **No full scan of `ntfy_notifications`** in the claim's plan, with work
   available or with none:
   - PostgreSQL: no `Seq Scan` on the table or its alias;
   - MySQL: no `access_type: ALL`;
   - SQLite: no `SCAN` without `USING`.
2. **An empty pass costs what the window holds, not what the table holds.**
   After another `N` rows are added outside the claim window, the empty-pass
   median must be at most `1.5 ×` the median before, plus 5 ms for timer noise.

The absolute anchors below apply to this machine and seed only, and are
checked on PostgreSQL:

3. An empty pass takes under 100 ms.
4. A pass claiming 500 candidates takes under 500 ms.

## Setup

- **Command:**

  ```
  cd sqlstore && NTFY_MEASURE_ROWS=1000000 GOTOOLCHAIN=go1.26.8 go test -run TestMeasureClaim -count=1 -timeout 90m -v .
  ```
- **Machine:** Apple M4 Pro, 14 cores, 24 GiB RAM, macOS 26.6.2, Go 1.26.8,
  Docker 29.8.0. PostgreSQL runs as `postgres:17.6-alpine` and MySQL as
  `mysql:8.4.6`, both through testcontainers. SQLite is modernc, on a file
  with WAL.
- **Seed, for `N` = 1,000,000:** 50,000 ACTIVE notifications spread across the
  default claim window (24 h max lag, 5 min grace). 45,000 (90%) of them
  already carry a terminal `SENT` delivery record. 950,000 ACTIVE
  notifications sit outside the window, and 1,000 recipients are spread
  evenly. The empty pass runs after the remaining 5,000 are also recorded
  `SENT`. The "grown" figure is taken after another 1,000,000 rows are added
  outside the window.
- **Samples:** each figure is the median of 7 claims. Anything a claim takes
  is deleted between samples.
- **Date:** 2026-09-27.

## Before

Run 1 is the reference run. Run 2 was the stability check. It overlapped with
other container-backed tests on the same machine, which is the likely cause of
its noisier SQLite figures.

| Dialect | Pass | Run 1 median | Run 2 median |
| --- | --- | --- | --- |
| PostgreSQL | working (500 claimed) | 93 ms | 114 ms |
| PostgreSQL | empty | 84 ms | 93 ms |
| PostgreSQL | empty, +1M rows outside the window | **163 ms** | **156 ms** |
| MySQL | working (500 claimed) | 1.83 s | 1.83 s |
| MySQL | empty | 1.87 s | 2.34 s |
| MySQL | empty, +1M rows outside the window | **3.58 s** | **3.80 s** |
| SQLite | working (500 claimed) | 637 ms | 991 ms |
| SQLite | empty | 681 ms | 745 ms |
| SQLite | empty, +1M rows outside the window | **1.28 s** | **3.29 s** |

**Every threshold fails on every dialect, in both runs.**

- Threshold 2: the limits in run 1 were PostgreSQL 131 ms, MySQL 2.81 s and
  SQLite 1.03 s. Adding a million rows the claim can never return roughly
  doubled the cost of a pass that claims nothing.
- Threshold 1: every plan scans `ntfy_notifications` in full. PostgreSQL shows
  `Seq Scan on ntfy_notifications n`, MySQL shows `access_type: ALL` on `n`,
  and SQLite shows `SCAN n`. Each also joins the deliveries table and sorts:
  PostgreSQL uses `Sort`, MySQL uses `using_filesort`, and SQLite uses a
  `TEMP B-TREE`.
- Threshold 3: the PostgreSQL empty pass came to 84 ms, under 100 ms at this
  size, but threshold 2 shows it grows with the table.

Both STOP gates (tasks 1.2 and 1.4) pass: the premise reproduces on all three
dialects.

### Plans for the empty pass, verbatim from run 1

#### PostgreSQL

```text
Limit  (cost=25833.61..25834.07 rows=4 width=72) (actual time=95.288..97.996 rows=0 loops=1)
  Buffers: shared hit=9322 read=14334
  ->  Gather Merge  (cost=25833.61..25834.07 rows=4 width=72) (actual time=95.287..97.995 rows=0 loops=1)
        Workers Planned: 2
        Workers Launched: 2
        Buffers: shared hit=9322 read=14334
        ->  Sort  (cost=24833.58..24833.59 rows=2 width=72) (actual time=93.934..93.935 rows=0 loops=3)
              Sort Key: n.created_at, n.id COLLATE "C"
              Sort Method: quicksort  Memory: 25kB
              Buffers: shared hit=9322 read=14334
              Worker 0:  Sort Method: quicksort  Memory: 25kB
              Worker 1:  Sort Method: quicksort  Memory: 25kB
              ->  Hash Left Join  (cost=1223.90..24833.57 rows=2 width=72) (actual time=93.787..93.788 rows=0 loops=3)
                    Hash Cond: (n.id = d.notification_id)
                    Filter: (((d.notification_id IS NULL) AND (n.state = 'ACTIVE'::text) AND (n.created_at <= '2026-09-27 15:53:16.189433+00'::timestamp with time zone) AND (n.created_at >= '2026-09-26 15:58:16.189433+00'::timestamp with time zone)) OR ((d.status = 'SENDING'::text) AND ((d.lease_until IS NULL) OR (d.lease_until <= '2026-09-27 15:58:16.189433+00'::timestamp with time zone)) AND (((d.status = ANY ('{CLAIMED,RETRY}'::text[])) AND ((d.next_attempt_at IS NULL) OR (d.next_attempt_at <= '2026-09-27 15:58:16.189433+00'::timestamp with time zone))) OR (d.status = 'SENDING'::text))) OR ((d.status <> 'SENDING'::text) AND ((d.lease_until IS NULL) OR (d.lease_until <= '2026-09-27 15:58:16.189433+00'::timestamp with time zone)) AND (((d.status = ANY ('{CLAIMED,RETRY}'::text[])) AND ((d.next_attempt_at IS NULL) OR (d.next_attempt_at <= '2026-09-27 15:58:16.189433+00'::timestamp with time zone))) OR (d.status = 'SENDING'::text)) AND (n.state = 'ACTIVE'::text) AND (n.created_at <= '2026-09-27 15:53:16.189433+00'::timestamp with time zone) AND (n.created_at >= '2026-09-26 15:58:16.189433+00'::timestamp with time zone)))
                    Rows Removed by Filter: 333333
                    Buffers: shared hit=9192 read=14334
                    ->  Parallel Seq Scan on ntfy_notifications n  (cost=0.00..23121.34 rows=186034 width=72) (actual time=0.026..19.568 rows=333333 loops=3)
                          Buffers: shared hit=6927 read=14334
                    ->  Hash  (cost=944.51..944.51 rows=22351 width=80) (actual time=8.025..8.026 rows=50000 loops=3)
                          Buckets: 65536 (originally 32768)  Batches: 1 (originally 1)  Memory Usage: 4126kB
                          Buffers: shared hit=2163
                          ->  Seq Scan on ntfy_email_deliveries d  (cost=0.00..944.51 rows=22351 width=80) (actual time=0.008..3.158 rows=50000 loops=3)
                                Buffers: shared hit=2163
Planning Time: 0.144 ms
Execution Time: 98.297 ms
```

#### MySQL

```json
{
  "query_block": {
    "select_id": 1,
    "cost_info": {
      "query_cost": "438963.76"
    },
    "ordering_operation": {
      "using_filesort": true,
      "nested_loop": [
        {
          "table": {
            "table_name": "n",
            "access_type": "ALL",
            "possible_keys": [
              "ntfy_notifications_inactive_idx"
            ],
            "rows_examined_per_scan": 965349,
            "rows_produced_per_join": 965349,
            "filtered": "100.00",
            "cost_info": {
              "read_cost": "4556.71",
              "eval_cost": "96534.90",
              "prefix_cost": "101091.61",
              "data_read_per_join": "4G"
            },
            "used_columns": [
              "id",
              "state",
              "created_at"
            ]
          }
        },
        {
          "table": {
            "table_name": "d",
            "access_type": "eq_ref",
            "possible_keys": [
              "PRIMARY"
            ],
            "key": "PRIMARY",
            "used_key_parts": [
              "notification_id"
            ],
            "key_length": "258",
            "ref": [
              "sqlkit.n.id"
            ],
            "rows_examined_per_scan": 1,
            "rows_produced_per_join": 965349,
            "filtered": "100.00",
            "cost_info": {
              "read_cost": "241337.25",
              "eval_cost": "96534.90",
              "prefix_cost": "438963.76",
              "data_read_per_join": "2G"
            },
            "used_columns": [
              "notification_id",
              "status",
              "lease_until",
              "next_attempt_at"
            ],
            "attached_condition": "<if>(found_match(d), (((`sqlkit`.`n`.`state` = 'ACTIVE') and (`sqlkit`.`d`.`notification_id` is null) and (`sqlkit`.`n`.`created_at` <= TIMESTAMP'2026-09-27 15:53:20.439124') and (`sqlkit`.`n`.`created_at` >= TIMESTAMP'2026-09-26 15:58:20.439124')) or ((`sqlkit`.`d`.`status` = 'SENDING') and ((`sqlkit`.`d`.`lease_until` is null) or (`sqlkit`.`d`.`lease_until` <= TIMESTAMP'2026-09-27 15:58:20.439124'))) or ((`sqlkit`.`n`.`state` = 'ACTIVE') and (`sqlkit`.`d`.`status` <> 'SENDING') and (((`sqlkit`.`d`.`status` in ('CLAIMED','RETRY')) and ((`sqlkit`.`d`.`lease_until` is null) or (`sqlkit`.`d`.`lease_until` <= TIMESTAMP'2026-09-27 15:58:20.439124')) and ((`sqlkit`.`d`.`next_attempt_at` is null) or (`sqlkit`.`d`.`next_attempt_at` <= TIMESTAMP'2026-09-27 15:58:20.439124'))) or ((`sqlkit`.`d`.`status` = 'SENDING') and ((`sqlkit`.`d`.`lease_until` is null) or (`sqlkit`.`d`.`lease_until` <= TIMESTAMP'2026-09-27 15:58:20.439124')))) and (`sqlkit`.`n`.`created_at` <= TIMESTAMP'2026-09-27 15:53:20.439124') and (`sqlkit`.`n`.`created_at` >= TIMESTAMP'2026-09-26 15:58:20.439124'))), true)"
          }
        }
      ]
    }
  }
}
```

#### SQLite

```text
SCAN n
SEARCH d USING INDEX sqlite_autoindex_ntfy_email_deliveries_1 (notification_id=?) LEFT-JOIN
USE TEMP B-TREE FOR ORDER BY
```

## After
