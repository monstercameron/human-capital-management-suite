# Where agent spend goes (AGENTCOST-001)

Measured from the review cell's run records, replayed as 42 per-call ledger lines at schedule schemaflux-2026-10-01. Each line records tokens in and out, cost and latency for one model call, classified as generation (text a person reads), extraction (structured output from content) or decision (one of a bounded set). Totals: 85985 tokens in, 4566 tokens out, $0.008952, 42 calls. 39 percent of measured spend is decisions.

## Spend per agent

| name           | calls | tokens in | tokens out |      cost | mean latency ms |
| -------------- | ----: | --------: | ---------: | --------: | --------------: |
| announcer      |     2 |      2700 |        208 | $0.000374 |             940 |
| policy-helper  |    22 |     69780 |       3769 | $0.007073 |            1275 |
| reminder       |     4 |      1105 |         96 | $0.000159 |             321 |
| support-triage |     4 |     10000 |        318 | $0.001016 |             887 |
| task-catcher   |    10 |      2400 |        175 |  $0.00033 |             272 |

## Spend per trigger kind

| name            | calls | tokens in | tokens out |      cost | mean latency ms |
| --------------- | ----: | --------: | ---------: | --------: | --------------: |
| ambient_message |    14 |      3505 |        271 | $0.000489 |             286 |
| email           |     4 |     10000 |        318 | $0.001016 |             887 |
| mention         |    20 |     57880 |       3672 | $0.005834 |            1217 |
| schedule        |     4 |     14600 |        305 | $0.001613 |            1395 |

## Spend per answered question

| name          | calls | tokens in | tokens out |      cost | mean latency ms |
| ------------- | ----: | --------: | ---------: | --------: | --------------: |
| (no question) |    22 |     28105 |        894 | $0.003118 |             597 |
| q1            |     3 |      7700 |        400 | $0.000727 |            1010 |
| q2            |     3 |      9300 |        638 | $0.001006 |            1176 |
| q3            |     4 |     11500 |        522 | $0.001024 |            1135 |
| q4            |     2 |      5550 |        552 | $0.000669 |            1400 |
| q5            |     2 |      6100 |        318 | $0.000553 |            1250 |
| q6            |     2 |      5950 |        481 | $0.000674 |            1360 |
| q7            |     2 |      6600 |        358 | $0.000623 |            1315 |
| q8            |     2 |      5180 |        403 | $0.000558 |            1302 |

## Ten most expensive call sites

| call site                              | calls |      cost |
| -------------------------------------- | ----: | --------: |
| persona_run_executor:answer            |     8 | $0.004085 |
| persona_eval:grade_answer              |     2 | $0.001239 |
| persona_run_executor:citation_check    |     5 | $0.000905 |
| support:draft_reply                    |     1 | $0.000444 |
| persona_run_executor:search_route      |     6 | $0.000443 |
| support:severity                       |     2 | $0.000414 |
| persona_run_executor:tool_continuation |     1 | $0.000401 |
| announce:draft                         |     1 |  $0.00025 |
| support:needs_review                   |     1 | $0.000158 |
| ambient:extract_task                   |     2 | $0.000154 |

## Kind split

Generation $0.00518, extraction $0.000242, decisions $0.00353. 39 percent of measured spend is decisions.

## Projection for the planned agents

Assumptions per person per day: 40 channel messages read (message screening at 150 input and 5 output tokens a message); the same 40 messages through to-do and reminder detection (450 input and 70 output tokens a message where a candidate is found, one message in twenty); 0.2 support emails (2,300 input and 25 output tokens with severity and review decisions, a drafted reply for one in two); 0.02 announcements (1,500 input and 200 output tokens). Thirty days a month, priced at schedule schemaflux-2026-10-01.

| people | screening | to-do and reminder detection | support triage | announcements | total per month |
| -----: | --------: | ---------------------------: | -------------: | ------------: | --------------: |
|     50 |     $1.08 |                        $0.24 |         $0.144 |       $0.0075 |         $1.4715 |
|    500 |     $10.8 |                         $2.4 |          $1.44 |        $0.075 |         $14.715 |
|  5,000 |      $108 |                          $24 |          $14.4 |         $0.75 |         $147.15 |

## How to regenerate

Run the seeded replay in internal/agentcost and render it: `RenderAgentcost001Report(Agentcost001SeededLines())`. Production lines come from `agentmodel.RecordingAdapter`, one line per model call, sealed in the run record beside the stage timings; the same lines rebuild from sealed runs with `Agentcost001LinesFromRunRecords`, so the numbers regenerate from the review cell's run records as well as the replay.
