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

The code is the three-branch claim with `ntfy_notifications_email_idx`
(commits `2eae6b6` and `68e9cbc`). The machine, seed and command are those
above.

| Dialect | Pass | Before (run 1) | After |
| --- | --- | --- | --- |
| PostgreSQL | working (500 claimed) | 93 ms | 85 ms |
| PostgreSQL | empty | 84 ms | 79 ms |
| PostgreSQL | empty, +1M rows outside the window | 163 ms | **80 ms** |
| MySQL | working (500 claimed) | 1.83 s | 68 ms |
| MySQL | empty | 1.87 s | 44 ms |
| MySQL | empty, +1M rows outside the window | 3.58 s | **44 ms** |
| SQLite | working (500 claimed) | 637 ms | 71 ms |
| SQLite | empty | 681 ms | 72 ms |
| SQLite | empty, +1M rows outside the window | 1.28 s | **70 ms** |

**Every threshold passes on every dialect** (`TestMeasureClaim` PASS):

- **1. No full scan.** No plan scans `ntfy_notifications` in full; the plans
  are below.
- **2. The window, not the table.** Adding a million rows outside the window
  leaves the empty pass unchanged: 79 → 80 ms, 44 → 44 ms and 72 → 70 ms,
  against limits of 124 ms, 71 ms and 113 ms.
- **3. An empty pass under 100 ms on PostgreSQL:** 79 ms.
- **4. A 500-candidate pass under 500 ms on PostgreSQL:** 85 ms.

**What remains is the residual in `design.md` D4.** PostgreSQL's empty pass
improved least, from 84 ms to 79 ms. Branch 1 still walks the 50,000 ACTIVE
notifications inside the window, and probes the deliveries key for each, so
the pass now costs what the window holds. Threshold 2 shows it no longer grows
with the table. D4 describes when the frontier cursor becomes worth building.

## Write-side cost (task 4.4)

The bulk seed is a one-transaction multi-row insert. Its time, with and
without the index, gives the index's cost to inserts. These are single runs
seeded in parallel across dialects, so the figures are indicative only.

| Dialect | 1M rows, no index (before) | 1M rows, with the index (after) | +1M outside rows, before → after |
| --- | --- | --- | --- |
| PostgreSQL | 20 s | 20 s | 19 s → 22 s |
| MySQL | 39 s | 42 s | 37 s → 39 s |
| SQLite | 68 s | 81 s | 57 s → 58 s |

The index costs at most about 20% on a bulk insert (the SQLite first seed),
and usually less than run-to-run noise. Only hosts that apply the email
schema pay it.

### Reading the plans

- **MySQL** still reports `access_type: ALL`, but only on the derived tables
  `b1`, `b2`, `b3` and `due`, each holding 504 rows at most. `n` is read
  through `ntfy_notifications_email_idx` (a range) or `PRIMARY` (`eq_ref`).
  **An observation, not a threshold:** MySQL materialises branch 1's
  anti-join by reading `ntfy_email_deliveries_lease_idx` in full, about
  50,000 entries here. That cost follows the deliveries table, which
  `PurgeEmailRecords` and retention keep bounded, not the notifications table,
  and threshold 2 still holds.
- **SQLite**'s `SCAN b1`, `SCAN b2`, `SCAN b3` and `SCAN due` read the
  co-routines of the compound query, not a table. Every table access is a
  `SEARCH ... USING (COVERING) INDEX`.
- **PostgreSQL** reads branch 1 as an `Index Only Scan using
  ntfy_notifications_email_idx` inside a `Nested Loop Anti Join`. The
  delivery-driven branches use the lease and retry indexes.

### Plans for the empty pass after the change

#### PostgreSQL

```text
Limit  (cost=458.78..741.84 rows=49 width=72) (actual time=86.289..86.294 rows=0 loops=1)
  Buffers: shared hit=403146
  ->  Merge Append  (cost=458.78..741.84 rows=49 width=72) (actual time=86.289..86.293 rows=0 loops=1)
        Sort Key: b1.created_at, b1.id COLLATE "C"
        Buffers: shared hit=403146
        ->  Subquery Scan on b1  (cost=0.83..141.51 rows=10 width=72) (actual time=41.990..41.992 rows=0 loops=1)
              Buffers: shared hit=201571
              ->  Limit  (cost=0.83..141.41 rows=10 width=72) (actual time=41.990..41.991 rows=0 loops=1)
                    Buffers: shared hit=201571
                    ->  Nested Loop Anti Join  (cost=0.83..141.41 rows=10 width=72) (actual time=41.989..41.990 rows=0 loops=1)
                          Buffers: shared hit=201571
                          ->  Index Only Scan using ntfy_notifications_email_idx on ntfy_notifications n  (cost=0.42..48.67 rows=11 width=40) (actual time=0.016..5.312 rows=50000 loops=1)
                                Index Cond: ((state = 'ACTIVE'::text) AND (created_at <= '2026-09-27 16:04:16.794311+00'::timestamp with time zone) AND (created_at >= '2026-09-26 16:09:16.794311+00'::timestamp with time zone))
                                Heap Fetches: 50000
                                Buffers: shared hit=1571
                          ->  Index Only Scan using ntfy_email_deliveries_pkey on ntfy_email_deliveries d  (cost=0.41..8.43 rows=1 width=32) (actual time=0.001..0.001 rows=1 loops=50000)
                                Index Cond: (notification_id = n.id)
                                Heap Fetches: 50000
                                Buffers: shared hit=200000
        ->  Subquery Scan on b2  (cost=457.09..457.56 rows=38 width=72) (actual time=0.034..0.036 rows=0 loops=1)
              Buffers: shared hit=4
              ->  Limit  (cost=457.09..457.18 rows=38 width=72) (actual time=0.033..0.035 rows=0 loops=1)
                    Buffers: shared hit=4
                    ->  Sort  (cost=457.09..457.18 rows=38 width=72) (actual time=0.032..0.034 rows=0 loops=1)
                          Sort Key: n_1.created_at, n_1.id COLLATE "C"
                          Sort Method: quicksort  Memory: 25kB
                          Buffers: shared hit=4
                          ->  Nested Loop  (cost=9.40..456.09 rows=38 width=72) (actual time=0.009..0.010 rows=0 loops=1)
                                Buffers: shared hit=4
                                ->  Bitmap Heap Scan on ntfy_email_deliveries d_1  (cost=8.97..135.37 rows=38 width=32) (actual time=0.009..0.010 rows=0 loops=1)
                                      Recheck Cond: (((status = 'SENDING'::text) AND (lease_until IS NULL)) OR ((status = 'SENDING'::text) AND (lease_until <= '2026-09-27 16:09:16.794311+00'::timestamp with time zone)))
                                      Buffers: shared hit=4
                                      ->  BitmapOr  (cost=8.97..8.97 rows=38 width=0) (actual time=0.005..0.005 rows=0 loops=1)
                                            Buffers: shared hit=4
                                            ->  Bitmap Index Scan on ntfy_email_deliveries_lease_idx  (cost=0.00..4.30 rows=1 width=0) (actual time=0.004..0.004 rows=0 loops=1)
                                                  Index Cond: ((status = 'SENDING'::text) AND (lease_until IS NULL))
                                                  Buffers: shared hit=2
                                            ->  Bitmap Index Scan on ntfy_email_deliveries_lease_idx  (cost=0.00..4.66 rows=37 width=0) (actual time=0.001..0.001 rows=0 loops=1)
                                                  Index Cond: ((status = 'SENDING'::text) AND (lease_until <= '2026-09-27 16:09:16.794311+00'::timestamp with time zone))
                                                  Buffers: shared hit=2
                                ->  Index Scan using ntfy_notifications_pkey on ntfy_notifications n_1  (cost=0.42..8.44 rows=1 width=40) (never executed)
                                      Index Cond: (id = d_1.notification_id)
        ->  Subquery Scan on b3  (cost=0.83..142.11 rows=1 width=72) (actual time=44.263..44.264 rows=0 loops=1)
              Buffers: shared hit=201571
              ->  Limit  (cost=0.83..142.10 rows=1 width=72) (actual time=44.263..44.263 rows=0 loops=1)
                    Buffers: shared hit=201571
                    ->  Nested Loop  (cost=0.83..142.10 rows=1 width=72) (actual time=44.262..44.262 rows=0 loops=1)
                          Buffers: shared hit=201571
                          ->  Index Only Scan using ntfy_notifications_email_idx on ntfy_notifications n_2  (cost=0.42..48.67 rows=11 width=40) (actual time=0.014..5.403 rows=50000 loops=1)
                                Index Cond: ((state = 'ACTIVE'::text) AND (created_at <= '2026-09-27 16:04:16.794311+00'::timestamp with time zone) AND (created_at >= '2026-09-26 16:09:16.794311+00'::timestamp with time zone))
                                Heap Fetches: 50000
                                Buffers: shared hit=1571
                          ->  Index Scan using ntfy_email_deliveries_pkey on ntfy_email_deliveries d_2  (cost=0.41..8.44 rows=1 width=32) (actual time=0.001..0.001 rows=0 loops=50000)
                                Index Cond: (notification_id = n_2.id)
                                Filter: ((status = ANY ('{CLAIMED,RETRY}'::text[])) AND ((lease_until IS NULL) OR (lease_until <= '2026-09-27 16:09:16.794311+00'::timestamp with time zone)) AND ((next_attempt_at IS NULL) OR (next_attempt_at <= '2026-09-27 16:09:16.794311+00'::timestamp with time zone)))
                                Rows Removed by Filter: 1
                                Buffers: shared hit=200000
Planning Time: 0.380 ms
Execution Time: 86.394 ms
```

#### MySQL

```json
{
  "query_block": {
    "select_id": 1,
    "cost_info": {
      "query_cost": "59.20"
    },
    "ordering_operation": {
      "using_filesort": true,
      "table": {
        "table_name": "due",
        "access_type": "ALL",
        "rows_examined_per_scan": 504,
        "rows_produced_per_join": 504,
        "filtered": "100.00",
        "cost_info": {
          "read_cost": "8.80",
          "eval_cost": "50.40",
          "prefix_cost": "59.20",
          "data_read_per_join": "137K"
        },
        "used_columns": [
          "id",
          "created_at",
          "recorded"
        ],
        "materialized_from_subquery": {
          "using_temporary_table": true,
          "dependent": false,
          "cacheable": true,
          "query_block": {
            "union_result": {
              "using_temporary_table": false,
              "query_specifications": [
                {
                  "dependent": false,
                  "cacheable": true,
                  "query_block": {
                    "select_id": 2,
                    "cost_info": {
                      "query_cost": "58.75"
                    },
                    "table": {
                      "table_name": "b1",
                      "access_type": "ALL",
                      "rows_examined_per_scan": 500,
                      "rows_produced_per_join": 500,
                      "filtered": "100.00",
                      "cost_info": {
                        "read_cost": "8.75",
                        "eval_cost": "50.00",
                        "prefix_cost": "58.75",
                        "data_read_per_join": "136K"
                      },
                      "used_columns": [
                        "id",
                        "created_at",
                        "recorded"
                      ],
                      "materialized_from_subquery": {
                        "using_temporary_table": true,
                        "dependent": false,
                        "cacheable": true,
                        "query_block": {
                          "select_id": 3,
                          "cost_info": {
                            "query_cost": "47463.38"
                          },
                          "ordering_operation": {
                            "using_filesort": false,
                            "nested_loop": [
                              {
                                "table": {
                                  "table_name": "n",
                                  "access_type": "range",
                                  "possible_keys": [
                                    "ntfy_notifications_inactive_idx",
                                    "ntfy_notifications_email_idx"
                                  ],
                                  "key": "ntfy_notifications_email_idx",
                                  "used_key_parts": [
                                    "state",
                                    "created_at"
                                  ],
                                  "key_length": "74",
                                  "rows_examined_per_scan": 102256,
                                  "rows_produced_per_join": 102256,
                                  "filtered": "100.00",
                                  "using_index": true,
                                  "cost_info": {
                                    "read_cost": "16943.93",
                                    "eval_cost": "10225.60",
                                    "prefix_cost": "27169.53",
                                    "data_read_per_join": "477M"
                                  },
                                  "used_columns": [
                                    "id",
                                    "state",
                                    "created_at"
                                  ],
                                  "attached_condition": "((`sqlkit`.`n`.`state` = 'ACTIVE') and (`sqlkit`.`n`.`created_at` <= TIMESTAMP'2026-09-27 16:04:20.934283') and (`sqlkit`.`n`.`created_at` >= TIMESTAMP'2026-09-26 16:09:20.934283'))"
                                }
                              },
                              {
                                "table": {
                                  "table_name": "<subquery4>",
                                  "access_type": "eq_ref",
                                  "key": "<auto_distinct_key>",
                                  "key_length": "259",
                                  "ref": [
                                    "sqlkit.n.id"
                                  ],
                                  "rows_examined_per_scan": 1,
                                  "not_exists": true,
                                  "attached_condition": "<if>(is_not_null_compl(<subquery4>), <if>(found_match(<subquery4>), false, true), true)",
                                  "materialized_from_subquery": {
                                    "using_temporary_table": true,
                                    "query_block": {
                                      "table": {
                                        "table_name": "d",
                                        "access_type": "index",
                                        "possible_keys": [
                                          "PRIMARY"
                                        ],
                                        "key": "ntfy_email_deliveries_lease_idx",
                                        "used_key_parts": [
                                          "status",
                                          "lease_until"
                                        ],
                                        "key_length": "75",
                                        "rows_examined_per_scan": 49975,
                                        "rows_produced_per_join": 49975,
                                        "filtered": "100.00",
                                        "using_index": true,
                                        "cost_info": {
                                          "read_cost": "72.25",
                                          "eval_cost": "4997.50",
                                          "prefix_cost": "5069.75",
                                          "data_read_per_join": "127M"
                                        },
                                        "used_columns": [
                                          "notification_id"
                                        ]
                                      }
                                    }
                                  }
                                }
                              }
                            ]
                          }
                        }
                      }
                    }
                  }
                },
                {
                  "dependent": false,
                  "cacheable": true,
                  "query_block": {
                    "select_id": 5,
                    "cost_info": {
                      "query_cost": "2.72"
                    },
                    "table": {
                      "table_name": "b2",
                      "access_type": "ALL",
                      "rows_examined_per_scan": 2,
                      "rows_produced_per_join": 2,
                      "filtered": "100.00",
                      "cost_info": {
                        "read_cost": "2.52",
                        "eval_cost": "0.20",
                        "prefix_cost": "2.73",
                        "data_read_per_join": "560"
                      },
                      "used_columns": [
                        "id",
                        "created_at",
                        "recorded"
                      ],
                      "materialized_from_subquery": {
                        "using_temporary_table": true,
                        "dependent": false,
                        "cacheable": true,
                        "query_block": {
                          "select_id": 6,
                          "cost_info": {
                            "query_cost": "1.18"
                          },
                          "ordering_operation": {
                            "using_temporary_table": true,
                            "using_filesort": true,
                            "cost_info": {
                              "sort_cost": "0.40"
                            },
                            "nested_loop": [
                              {
                                "table": {
                                  "table_name": "d",
                                  "access_type": "ref",
                                  "possible_keys": [
                                    "PRIMARY",
                                    "ntfy_email_deliveries_lease_idx",
                                    "ntfy_email_deliveries_retry_idx"
                                  ],
                                  "key": "ntfy_email_deliveries_retry_idx",
                                  "used_key_parts": [
                                    "status"
                                  ],
                                  "key_length": "66",
                                  "ref": [
                                    "const"
                                  ],
                                  "rows_examined_per_scan": 1,
                                  "rows_produced_per_join": 0,
                                  "filtered": "40.00",
                                  "cost_info": {
                                    "read_cost": "0.25",
                                    "eval_cost": "0.04",
                                    "prefix_cost": "0.35",
                                    "data_read_per_join": "1K"
                                  },
                                  "used_columns": [
                                    "notification_id",
                                    "status",
                                    "lease_until"
                                  ],
                                  "attached_condition": "((`sqlkit`.`d`.`lease_until` is null) or (`sqlkit`.`d`.`lease_until` <= TIMESTAMP'2026-09-27 16:09:20.934283'))"
                                }
                              },
                              {
                                "table": {
                                  "table_name": "n",
                                  "access_type": "eq_ref",
                                  "possible_keys": [
                                    "PRIMARY"
                                  ],
                                  "key": "PRIMARY",
                                  "used_key_parts": [
                                    "id"
                                  ],
                                  "key_length": "258",
                                  "ref": [
                                    "sqlkit.d.notification_id"
                                  ],
                                  "rows_examined_per_scan": 1,
                                  "rows_produced_per_join": 0,
                                  "filtered": "100.00",
                                  "cost_info": {
                                    "read_cost": "0.39",
                                    "eval_cost": "0.04",
                                    "prefix_cost": "0.78",
                                    "data_read_per_join": "1K"
                                  },
                                  "used_columns": [
                                    "id",
                                    "created_at"
                                  ]
                                }
                              }
                            ]
                          }
                        }
                      }
                    }
                  }
                },
                {
                  "dependent": false,
                  "cacheable": true,
                  "query_block": {
                    "select_id": 7,
                    "cost_info": {
                      "query_cost": "2.72"
                    },
                    "table": {
                      "table_name": "b3",
                      "access_type": "ALL",
                      "rows_examined_per_scan": 2,
                      "rows_produced_per_join": 2,
                      "filtered": "100.00",
                      "cost_info": {
                        "read_cost": "2.52",
                        "eval_cost": "0.20",
                        "prefix_cost": "2.73",
                        "data_read_per_join": "560"
                      },
                      "used_columns": [
                        "id",
                        "created_at",
                        "recorded"
                      ],
                      "materialized_from_subquery": {
                        "using_temporary_table": true,
                        "dependent": false,
                        "cacheable": true,
                        "query_block": {
                          "select_id": 8,
                          "cost_info": {
                            "query_cost": "2.31"
                          },
                          "ordering_operation": {
                            "using_temporary_table": true,
                            "using_filesort": true,
                            "cost_info": {
                              "sort_cost": "0.04"
                            },
                            "nested_loop": [
                              {
                                "table": {
                                  "table_name": "d",
                                  "access_type": "range",
                                  "possible_keys": [
                                    "PRIMARY",
                                    "ntfy_email_deliveries_lease_idx",
                                    "ntfy_email_deliveries_retry_idx"
                                  ],
                                  "key": "ntfy_email_deliveries_lease_idx",
                                  "used_key_parts": [
                                    "status",
                                    "lease_until"
                                  ],
                                  "key_length": "75",
                                  "rows_examined_per_scan": 2,
                                  "rows_produced_per_join": 0,
                                  "filtered": "40.00",
                                  "index_condition": "((`sqlkit`.`d`.`status` in ('CLAIMED','RETRY')) and ((`sqlkit`.`d`.`lease_until` is null) or (`sqlkit`.`d`.`lease_until` <= TIMESTAMP'2026-09-27 16:09:20.934283')))",
                                  "cost_info": {
                                    "read_cost": "1.33",
                                    "eval_cost": "0.08",
                                    "prefix_cost": "1.41",
                                    "data_read_per_join": "2K"
                                  },
                                  "used_columns": [
                                    "notification_id",
                                    "status",
                                    "lease_until",
                                    "next_attempt_at"
                                  ],
                                  "attached_condition": "((`sqlkit`.`d`.`next_attempt_at` is null) or (`sqlkit`.`d`.`next_attempt_at` <= TIMESTAMP'2026-09-27 16:09:20.934283'))"
                                }
                              },
                              {
                                "table": {
                                  "table_name": "n",
                                  "access_type": "eq_ref",
                                  "possible_keys": [
                                    "PRIMARY",
                                    "ntfy_notifications_inactive_idx",
                                    "ntfy_notifications_email_idx"
                                  ],
                                  "key": "PRIMARY",
                                  "used_key_parts": [
                                    "id"
                                  ],
                                  "key_length": "258",
                                  "ref": [
                                    "sqlkit.d.notification_id"
                                  ],
                                  "rows_examined_per_scan": 1,
                                  "rows_produced_per_join": 0,
                                  "filtered": "5.55",
                                  "cost_info": {
                                    "read_cost": "0.78",
                                    "eval_cost": "0.00",
                                    "prefix_cost": "2.27",
                                    "data_read_per_join": "217"
                                  },
                                  "used_columns": [
                                    "id",
                                    "state",
                                    "created_at"
                                  ],
                                  "attached_condition": "((`sqlkit`.`n`.`state` = 'ACTIVE') and (`sqlkit`.`n`.`created_at` <= TIMESTAMP'2026-09-27 16:04:20.934283') and (`sqlkit`.`n`.`created_at` >= TIMESTAMP'2026-09-26 16:09:20.934283'))"
                                }
                              }
                            ]
                          }
                        }
                      }
                    }
                  }
                }
              ]
            }
          }
        }
      }
    }
  }
}
```

#### SQLite

```text
CO-ROUTINE due
COMPOUND QUERY
LEFT-MOST SUBQUERY
CO-ROUTINE b1
SEARCH n USING COVERING INDEX ntfy_notifications_email_idx (state=? AND created_at>? AND created_at<?)
CORRELATED SCALAR SUBQUERY 1
SEARCH d USING COVERING INDEX sqlite_autoindex_ntfy_email_deliveries_1 (notification_id=?)
SCAN b1
UNION ALL
CO-ROUTINE b2
SEARCH d USING INDEX ntfy_email_deliveries_retry_idx (status=?)
SEARCH n USING INDEX sqlite_autoindex_ntfy_notifications_1 (id=?)
USE TEMP B-TREE FOR ORDER BY
SCAN b2
UNION ALL
CO-ROUTINE b3
SEARCH n USING COVERING INDEX ntfy_notifications_email_idx (state=? AND created_at>? AND created_at<?)
SEARCH d USING INDEX sqlite_autoindex_ntfy_email_deliveries_1 (notification_id=?)
SCAN b3
SCAN due
USE TEMP B-TREE FOR ORDER BY
```
