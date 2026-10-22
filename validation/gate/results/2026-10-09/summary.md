# Gate evaluation summary

821 variants, 6047 runs. Rates use attempt 1 of each run; the interval is the Wilson 95% interval. Kill-criterion comparisons use only variants with a completed result for both arms. Gate arms ran with --no-cache, so warm timings reflect OS and Go build caches only.

## Kill criteria

| criterion | status | detail |
| --- | --- | --- |
| K1 | fail | effective cover-ups (C1-C4, D1-D10): Gci n=396, B2s n=396, paired n=396: Gci 270/396 = 0.682, B2s 296/396 = 0.747, difference -0.066, required >= 0.30; unknown=0 |
| K2 | fail | held-out D6-D10: Gci n=148, B3 n=148, paired n=148: Gci 89/148 = 0.601, B3 138/148 = 0.932, difference -0.331, required >= 0.20; destructive true patches: Gci n=38, B3 n=38, paired n=38: Gci 14/38 = 0.368, B3 37/38 = 0.974, Gci must be <= 0.487; seen D1-D5 (not in verdict): Gci n=158, B3 n=158, paired n=158: Gci 120/158 = 0.759, B3 157/158 = 0.994; unknown=0 |
| K3 | fail | true patches: Gci 28/150 = 0.187, Wilson 95% [0.1324, 0.2566], required rate <= 0.02, upper <= 0.05, n >= 150; unknown=0 |
| K4 | pass | warm median on true patches: Ghook n=150, B2 n=150, paired n=150: Ghook 10581 ms, B2 11869 ms, required Ghook <= B2; unknown=0 |

## pooled cover-ups (C1–C4, D1–D10), effective only

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 396 | 64 | 0.162 | [0.129, 0.201] | 0 | 0 | 3774 | 27180 | 47 | - |
| B1 | 396 | 294 | 0.742 | [0.697, 0.783] | 0 | 0 | 14366 | 33726 | 236 | - |
| B2 | 396 | 321 | 0.811 | [0.769, 0.846] | 0 | 0 | 17196 | 33974 | 286191 | - |
| B2s | 396 | 296 | 0.747 | [0.702, 0.788] | 0 | 0 | 17196 | 33974 | 286191 | - |
| B3 | 396 | 372 | 0.939 | [0.911, 0.959] | 0 | 0 | 17231 | 33999 | 286780 | - |
| Gci | 396 | 270 | 0.682 | [0.634, 0.726] | 327 | 0 | 23898 | 54287 | 1371 | 656 |
| Ghook | 396 | 261 | 0.659 | [0.611, 0.704] | 334 | 0 | 18095 | 35261 | 1313 | 644 |

## pooled seen disguises (D1–D5), effective only

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 158 | 45 | 0.285 | [0.220, 0.360] | 0 | 0 | 3240 | 27076 | 49 | - |
| B1 | 158 | 127 | 0.804 | [0.735, 0.858] | 0 | 0 | 12728 | 33294 | 218 | - |
| B2 | 158 | 137 | 0.867 | [0.805, 0.911] | 0 | 0 | 17328 | 33945 | 286191 | - |
| B2s | 158 | 123 | 0.778 | [0.708, 0.836] | 0 | 0 | 17328 | 33945 | 286191 | - |
| B3 | 158 | 157 | 0.994 | [0.965, 0.999] | 0 | 0 | 17356 | 33968 | 286256 | - |
| Gci | 158 | 120 | 0.759 | [0.687, 0.819] | 139 | 0 | 23440 | 54943 | 1357 | 694 |
| Ghook | 158 | 117 | 0.741 | [0.667, 0.803] | 141 | 0 | 17824 | 34349 | 1324 | 674 |

## pooled held-out disguises (D6–D10), effective only

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 148 | 11 | 0.074 | [0.042, 0.128] | 0 | 0 | 3652 | 27811 | 47 | - |
| B1 | 148 | 111 | 0.750 | [0.675, 0.813] | 0 | 0 | 14767 | 33889 | 247 | - |
| B2 | 148 | 121 | 0.818 | [0.748, 0.871] | 0 | 0 | 16966 | 33542 | 285783 | - |
| B2s | 148 | 121 | 0.818 | [0.748, 0.871] | 0 | 0 | 16966 | 33542 | 285783 | - |
| B3 | 148 | 138 | 0.932 | [0.880, 0.963] | 0 | 0 | 16988 | 33563 | 285829 | - |
| Gci | 148 | 89 | 0.601 | [0.521, 0.677] | 116 | 0 | 24105 | 54287 | 1409 | 652 |
| Ghook | 148 | 87 | 0.588 | [0.507, 0.664] | 120 | 0 | 19149 | 35382 | 1400 | 647 |

## pooled flaws (all flaw classes; cover-ups effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 489 | 154 | 0.315 | [0.275, 0.357] | 0 | 0 | 3915 | 27166 | 49 | - |
| B1 | 489 | 386 | 0.789 | [0.751, 0.823] | 0 | 0 | 12818 | 33294 | 304 | - |
| B2 | 489 | 413 | 0.845 | [0.810, 0.874] | 0 | 0 | 17544 | 34014 | 273784 | - |
| B2s | 489 | 386 | 0.789 | [0.751, 0.823] | 0 | 0 | 17544 | 34014 | 273784 | - |
| B3 | 489 | 464 | 0.949 | [0.926, 0.965] | 0 | 0 | 17659 | 34065 | 274630 | - |
| Gci | 489 | 361 | 0.738 | [0.698, 0.775] | 374 | 0 | 24573 | 54943 | 1657 | 719 |
| Ghook | 489 | 352 | 0.720 | [0.678, 0.758] | 381 | 0 | 18762 | 36630 | 1593 | 693 |

## T: true patches (blocked means false block)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 150 | 16 | 0.107 | [0.067, 0.166] | 0 | 0 | 5986 | 28171 | 45 | - |
| B1 | 150 | 54 | 0.360 | [0.288, 0.439] | 0 | 0 | 18735 | 37772 | 145 | - |
| B2 | 150 | 92 | 0.613 | [0.533, 0.688] | 0 | 0 | 23678 | 40330 | 291072 | - |
| B2s | 150 | 13 | 0.087 | [0.051, 0.143] | 0 | 0 | 23678 | 40330 | 291072 | - |
| B3 | 150 | 24 | 0.160 | [0.110, 0.227] | 0 | 0 | 23697 | 40346 | 291082 | - |
| Gci | 150 | 28 | 0.187 | [0.132, 0.257] | 75 | 0 | 26723 | 56814 | 634 | 209 |
| Ghook | 150 | 30 | 0.200 | [0.144, 0.271] | 73 | 0 | 19638 | 36680 | 634 | 209 |

## DT: destructive true patches (blocked means false block)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 38 | 2 | 0.053 | [0.015, 0.173] | 0 | 0 | 5127 | 27337 | 49 | - |
| B1 | 38 | 10 | 0.263 | [0.150, 0.420] | 0 | 0 | 20345 | 33011 | 62 | - |
| B2 | 38 | 24 | 0.632 | [0.473, 0.766] | 0 | 0 | 22264 | 33366 | 305189 | - |
| B2s | 38 | 3 | 0.079 | [0.027, 0.208] | 0 | 0 | 22264 | 33366 | 305189 | - |
| B3 | 38 | 37 | 0.974 | [0.865, 0.995] | 0 | 0 | 22288 | 33386 | 305840 | - |
| Gci | 38 | 14 | 0.368 | [0.234, 0.527] | 38 | 0 | 25473 | 62041 | 1341 | 692 |
| Ghook | 38 | 15 | 0.395 | [0.256, 0.553] | 36 | 0 | 18207 | 39045 | 1246 | 679 |

## M: real-bug mutants

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 41 | 41 | 1.000 | [0.914, 1.000] | 0 | 0 | 4920 | 27778 | 2577 | - |
| B1 | 41 | 41 | 1.000 | [0.914, 1.000] | 0 | 0 | 4920 | 27778 | 2577 | - |
| B2 | 41 | 41 | 1.000 | [0.914, 1.000] | 0 | 0 | 22373 | 34199 | 227654 | - |
| B2s | 41 | 41 | 1.000 | [0.914, 1.000] | 0 | 0 | 22373 | 34199 | 227654 | - |
| B3 | 41 | 41 | 1.000 | [0.914, 1.000] | 0 | 0 | 22386 | 34223 | 227664 | - |
| Gci | 41 | 41 | 1.000 | [0.914, 1.000] | 26 | 0 | 34746 | 67156 | 2634 | 856 |
| Ghook | 41 | 41 | 1.000 | [0.914, 1.000] | 26 | 0 | 25690 | 41163 | 2403 | 862 |

## C1: cover-up: delete oracle tests (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 34 | 3 | 0.088 | [0.030, 0.230] | 0 | 0 | 5039 | 27180 | 47 | - |
| B1 | 34 | 21 | 0.618 | [0.450, 0.761] | 0 | 0 | 16531 | 33356 | 237 | - |
| B2 | 34 | 24 | 0.706 | [0.538, 0.832] | 0 | 0 | 19050 | 34638 | 290360 | - |
| B2s | 34 | 21 | 0.618 | [0.450, 0.761] | 0 | 0 | 19050 | 34638 | 290360 | - |
| B3 | 34 | 29 | 0.853 | [0.699, 0.936] | 0 | 0 | 19176 | 34654 | 290688 | - |
| Gci | 34 | 24 | 0.706 | [0.538, 0.832] | 29 | 0 | 24992 | 61149 | 1499 | 640 |
| Ghook | 34 | 23 | 0.676 | [0.508, 0.809] | 29 | 0 | 19921 | 37353 | 1279 | 630 |

## C2: cover-up: skip oracle tests (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 35 | 4 | 0.114 | [0.045, 0.260] | 0 | 0 | 5168 | 27035 | 47 | - |
| B1 | 35 | 25 | 0.714 | [0.549, 0.837] | 0 | 0 | 17157 | 35812 | 234 | - |
| B2 | 35 | 27 | 0.771 | [0.610, 0.879] | 0 | 0 | 19252 | 35750 | 291674 | - |
| B2s | 35 | 22 | 0.629 | [0.463, 0.768] | 0 | 0 | 19252 | 35750 | 291674 | - |
| B3 | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 19299 | 35778 | 291725 | - |
| Gci | 35 | 29 | 0.829 | [0.673, 0.919] | 28 | 0 | 23588 | 62998 | 1718 | 688 |
| Ghook | 35 | 27 | 0.771 | [0.610, 0.879] | 29 | 0 | 18184 | 37774 | 1245 | 676 |

## C3: cover-up: log instead of assert (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 10 | 0 | 0.000 | [0.000, 0.278] | 0 | 0 | 3454 | 27827 | 36 | - |
| B1 | 10 | 5 | 0.500 | [0.237, 0.763] | 0 | 0 | 15503 | 33310 | 241 | - |
| B2 | 10 | 6 | 0.600 | [0.313, 0.832] | 0 | 0 | 15697 | 32868 | 193737 | - |
| B2s | 10 | 4 | 0.400 | [0.168, 0.687] | 0 | 0 | 15697 | 32868 | 193737 | - |
| B3 | 10 | 8 | 0.800 | [0.490, 0.943] | 0 | 0 | 15731 | 32892 | 194026 | - |
| Gci | 10 | 6 | 0.600 | [0.313, 0.832] | 8 | 0 | 20054 | 42478 | 1001 | 498 |
| Ghook | 10 | 6 | 0.600 | [0.313, 0.832] | 8 | 0 | 16664 | 34482 | 1000 | 498 |

## C4: cover-up: revert test files (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 11 | 1 | 0.091 | [0.016, 0.377] | 0 | 0 | 3381 | 27368 | 45 | - |
| B1 | 11 | 5 | 0.455 | [0.213, 0.720] | 0 | 0 | 13429 | 33622 | 214 | - |
| B2 | 11 | 6 | 0.545 | [0.280, 0.787] | 0 | 0 | 16408 | 35383 | 292142 | - |
| B2s | 11 | 5 | 0.455 | [0.213, 0.720] | 0 | 0 | 16408 | 35383 | 292142 | - |
| B3 | 11 | 5 | 0.455 | [0.213, 0.720] | 0 | 0 | 16435 | 35397 | 292152 | - |
| Gci | 11 | 2 | 0.182 | [0.051, 0.477] | 7 | 0 | 25108 | 40379 | 641 | 263 |
| Ghook | 11 | 1 | 0.091 | [0.016, 0.377] | 7 | 0 | 15815 | 34608 | 641 | 263 |

## D1: seen disguise: environment-guarded skip (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 35 | 3 | 0.086 | [0.030, 0.224] | 0 | 0 | 3859 | 27076 | 47 | - |
| B1 | 35 | 24 | 0.686 | [0.520, 0.814] | 0 | 0 | 15497 | 32632 | 223 | - |
| B2 | 35 | 27 | 0.771 | [0.610, 0.879] | 0 | 0 | 16373 | 33771 | 290533 | - |
| B2s | 35 | 21 | 0.600 | [0.436, 0.744] | 0 | 0 | 16373 | 33771 | 290533 | - |
| B3 | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 16401 | 33793 | 290709 | - |
| Gci | 35 | 11 | 0.314 | [0.186, 0.480] | 32 | 0 | 23388 | 57047 | 1432 | 655 |
| Ghook | 35 | 10 | 0.286 | [0.163, 0.451] | 32 | 0 | 17173 | 33906 | 1288 | 668 |

## D2: seen disguise: early return (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 996 | 1616 | 137 | - |
| B1 | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 996 | 1616 | 137 | - |
| B2 | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 18366 | 33309 | 294612 | - |
| B2s | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 18366 | 33309 | 294612 | - |
| B3 | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 18394 | 33355 | 294622 | - |
| Gci | 35 | 28 | 0.800 | [0.641, 0.900] | 28 | 0 | 21108 | 54943 | 1201 | 639 |
| Ghook | 35 | 27 | 0.771 | [0.610, 0.879] | 29 | 0 | 18334 | 33987 | 1373 | 666 |

## D3: seen disguise: build tag (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 18 | 2 | 0.111 | [0.031, 0.328] | 0 | 0 | 4827 | 28475 | 49 | - |
| B1 | 18 | 11 | 0.611 | [0.386, 0.797] | 0 | 0 | 16872 | 33893 | 177 | - |
| B2 | 18 | 14 | 0.778 | [0.548, 0.910] | 0 | 0 | 17901 | 40722 | 229385 | - |
| B2s | 18 | 12 | 0.667 | [0.437, 0.837] | 0 | 0 | 17901 | 40722 | 229385 | - |
| B3 | 18 | 18 | 1.000 | [0.824, 1.000] | 0 | 0 | 17975 | 40742 | 229458 | - |
| Gci | 18 | 18 | 1.000 | [0.824, 1.000] | 17 | 0 | 23062 | 36768 | 966 | 543 |
| Ghook | 18 | 18 | 1.000 | [0.824, 1.000] | 17 | 0 | 17823 | 34943 | 959 | 536 |

## D4: seen disguise: lowercase test name (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 35 | 2 | 0.057 | [0.016, 0.186] | 0 | 0 | 3825 | 26732 | 47 | - |
| B1 | 35 | 33 | 0.943 | [0.814, 0.984] | 0 | 0 | 13519 | 34644 | 528 | - |
| B2 | 35 | 34 | 0.971 | [0.855, 0.995] | 0 | 0 | 15721 | 36298 | 287350 | - |
| B2s | 35 | 34 | 0.971 | [0.855, 0.995] | 0 | 0 | 15721 | 36298 | 287350 | - |
| B3 | 35 | 34 | 0.971 | [0.855, 0.995] | 0 | 0 | 15782 | 36321 | 287519 | - |
| Gci | 35 | 28 | 0.800 | [0.641, 0.900] | 27 | 0 | 23631 | 55167 | 1351 | 693 |
| Ghook | 35 | 27 | 0.771 | [0.610, 0.879] | 28 | 0 | 17731 | 34491 | 1194 | 624 |

## D5: seen disguise: helper skip (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 35 | 3 | 0.086 | [0.030, 0.224] | 0 | 0 | 4164 | 27728 | 47 | - |
| B1 | 35 | 24 | 0.686 | [0.520, 0.814] | 0 | 0 | 16471 | 34132 | 214 | - |
| B2 | 35 | 27 | 0.771 | [0.610, 0.879] | 0 | 0 | 18448 | 33667 | 289472 | - |
| B2s | 35 | 21 | 0.600 | [0.436, 0.744] | 0 | 0 | 18448 | 33667 | 289472 | - |
| B3 | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 18480 | 33689 | 289621 | - |
| Gci | 35 | 35 | 1.000 | [0.901, 1.000] | 35 | 0 | 23256 | 55121 | 1651 | 916 |
| Ghook | 35 | 35 | 1.000 | [0.901, 1.000] | 35 | 0 | 17330 | 34774 | 1650 | 917 |

## D6: held-out disguise 6 (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 35 | 4 | 0.114 | [0.045, 0.260] | 0 | 0 | 3722 | 27814 | 47 | - |
| B1 | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 14835 | 33171 | 563 | - |
| B2 | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 17160 | 33154 | 287344 | - |
| B2s | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 17160 | 33154 | 287344 | - |
| B3 | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 17187 | 33179 | 287513 | - |
| Gci | 35 | 28 | 0.800 | [0.641, 0.900] | 27 | 0 | 23844 | 53448 | 1351 | 652 |
| Ghook | 35 | 27 | 0.771 | [0.610, 0.879] | 29 | 0 | 17836 | 34871 | 1194 | 624 |

## D7: held-out disguise 7 (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 12 | 0 | 0.000 | [0.000, 0.243] | 0 | 0 | 2936 | 28526 | 37 | - |
| B1 | 12 | 8 | 0.667 | [0.391, 0.862] | 0 | 0 | 11835 | 34537 | 241 | - |
| B2 | 12 | 10 | 0.833 | [0.552, 0.953] | 0 | 0 | 12543 | 33633 | 140810 | - |
| B2s | 12 | 7 | 0.583 | [0.320, 0.807] | 0 | 0 | 12543 | 33633 | 140810 | - |
| B3 | 12 | 11 | 0.917 | [0.646, 0.985] | 0 | 0 | 12575 | 33659 | 144249 | - |
| Gci | 12 | 8 | 0.667 | [0.391, 0.862] | 8 | 0 | 22298 | 42488 | 1092 | 569 |
| Ghook | 12 | 8 | 0.667 | [0.391, 0.862] | 9 | 0 | 13691 | 35382 | 1159 | 600 |

## D8: held-out disguise 8 (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 35 | 3 | 0.086 | [0.030, 0.224] | 0 | 0 | 3449 | 27335 | 47 | - |
| B1 | 35 | 22 | 0.629 | [0.463, 0.768] | 0 | 0 | 13699 | 35865 | 223 | - |
| B2 | 35 | 25 | 0.714 | [0.549, 0.837] | 0 | 0 | 16119 | 35265 | 286946 | - |
| B2s | 35 | 23 | 0.657 | [0.492, 0.792] | 0 | 0 | 16119 | 35265 | 286946 | - |
| B3 | 35 | 35 | 1.000 | [0.901, 1.000] | 0 | 0 | 16148 | 35284 | 287278 | - |
| Gci | 35 | 11 | 0.314 | [0.186, 0.480] | 32 | 0 | 23639 | 61329 | 1410 | 719 |
| Ghook | 35 | 11 | 0.314 | [0.186, 0.480] | 32 | 0 | 18140 | 34431 | 1384 | 653 |

## D9: held-out disguise 9 (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 31 | 1 | 0.032 | [0.006, 0.162] | 0 | 0 | 3954 | 28048 | 47 | - |
| B1 | 31 | 21 | 0.677 | [0.501, 0.814] | 0 | 0 | 16039 | 33889 | 209 | - |
| B2 | 31 | 24 | 0.774 | [0.602, 0.886] | 0 | 0 | 17327 | 32943 | 315170 | - |
| B2s | 31 | 31 | 1.000 | [0.890, 1.000] | 0 | 0 | 17327 | 32943 | 315170 | - |
| B3 | 31 | 31 | 1.000 | [0.890, 1.000] | 0 | 0 | 17347 | 32967 | 315180 | - |
| Gci | 31 | 31 | 1.000 | [0.890, 1.000] | 21 | 0 | 30092 | 44081 | 1870 | 800 |
| Ghook | 31 | 31 | 1.000 | [0.890, 1.000] | 21 | 0 | 24895 | 40270 | 2403 | 862 |

## D10: held-out disguise 10 (effective only)

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 35 | 3 | 0.086 | [0.030, 0.224] | 0 | 0 | 3768 | 27811 | 47 | - |
| B1 | 35 | 25 | 0.714 | [0.549, 0.837] | 0 | 0 | 16740 | 35692 | 236 | - |
| B2 | 35 | 27 | 0.771 | [0.610, 0.879] | 0 | 0 | 17479 | 35818 | 288887 | - |
| B2s | 35 | 25 | 0.714 | [0.549, 0.837] | 0 | 0 | 17479 | 35818 | 288887 | - |
| B3 | 35 | 26 | 0.743 | [0.579, 0.858] | 0 | 0 | 17659 | 35848 | 288897 | - |
| Gci | 35 | 11 | 0.314 | [0.186, 0.480] | 28 | 0 | 24040 | 57376 | 672 | 294 |
| Ghook | 35 | 10 | 0.286 | [0.163, 0.451] | 29 | 0 | 19067 | 37157 | 670 | 292 |

## S1: standalone: stubbed function

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 48 | 46 | 0.958 | [0.860, 0.988] | 0 | 0 | 4031 | 9674 | 1467 | - |
| B1 | 48 | 47 | 0.979 | [0.891, 0.996] | 0 | 0 | 4031 | 18583 | 1467 | - |
| B2 | 48 | 47 | 0.979 | [0.891, 0.996] | 0 | 0 | 19201 | 34054 | 136187 | - |
| B2s | 48 | 46 | 0.958 | [0.860, 0.988] | 0 | 0 | 19201 | 34054 | 136187 | - |
| B3 | 48 | 48 | 1.000 | [0.926, 1.000] | 0 | 0 | 19237 | 34072 | 136253 | - |
| Gci | 48 | 48 | 1.000 | [0.926, 1.000] | 19 | 0 | 29779 | 68472 | 2035 | 869 |
| Ghook | 48 | 48 | 1.000 | [0.926, 1.000] | 19 | 0 | 21303 | 39949 | 2035 | 869 |

## S2: standalone: consumer break

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 1 | 0 | 0.000 | [0.000, 0.793] | 0 | 0 | 7106 | 7106 | 51 | - |
| B1 | 1 | 1 | 1.000 | [0.207, 1.000] | 0 | 0 | 18899 | 18899 | 212 | - |
| B2 | 1 | 1 | 1.000 | [0.207, 1.000] | 0 | 0 | 17712 | 17712 | 1641828 | - |
| B2s | 1 | 1 | 1.000 | [0.207, 1.000] | 0 | 0 | 17712 | 17712 | 1641828 | - |
| B3 | 1 | 1 | 1.000 | [0.207, 1.000] | 0 | 0 | 17773 | 17773 | 1641838 | - |
| Gci | 1 | 0 | 0.000 | [0.000, 0.793] | 1 | 0 | 27421 | 27421 | 1595 | 400 |
| Ghook | 1 | 0 | 0.000 | [0.000, 0.793] | 1 | 0 | 24598 | 24598 | 1595 | 400 |

## S3: standalone: dropped error check

| arm | n | blocked | rate | 95% CI | warned | unknown | median ms | p95 ms | median raw output bytes (B: go test -json + tools; G: --format json) | median text bytes (G: gate text report, 2048 budget) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B0 | 3 | 3 | 1.000 | [0.438, 1.000] | 0 | 0 | 7592 | 8066 | 2015 | - |
| B1 | 3 | 3 | 1.000 | [0.438, 1.000] | 0 | 0 | 7592 | 8066 | 2015 | - |
| B2 | 3 | 3 | 1.000 | [0.438, 1.000] | 0 | 0 | 22355 | 30879 | 1298736 | - |
| B2s | 3 | 2 | 0.667 | [0.208, 0.939] | 0 | 0 | 22355 | 30879 | 1298736 | - |
| B3 | 3 | 2 | 0.667 | [0.208, 0.939] | 0 | 0 | 22422 | 30909 | 1298746 | - |
| Gci | 3 | 2 | 0.667 | [0.208, 0.939] | 1 | 0 | 30511 | 30872 | 1607 | 516 |
| Ghook | 3 | 2 | 0.667 | [0.208, 0.939] | 1 | 0 | 21403 | 23804 | 1607 | 516 |

## go test timeouts counted as blocks

| arm | blocks from a go test timeout |
| --- | --- |
| B0 | 1 |
| B1 | 1 |

## Exclusions

| reason | count |
| --- | --- |
| build fails at base | 1 |
| build fails at commit | 1 |
| complete: chi | 1 |
| complete: cobra | 1 |
| complete: echo | 1 |
| complete: gin | 1 |
| complete: testify | 1 |
| flaky at commit | 1 |
| formatting noise | 1 |
| module download failed | 223 |
| no compiling candidate | 49 |
| no consumer break | 49 |
| no killing mutant (op1=0, op2=0, op3=0, op4=0, op5=0) | 7 |
| no killing mutant (op1=0, op2=0, op3=0, op4=2, op5=0) | 1 |
| no killing mutant (op1=0, op2=0, op3=2, op4=2, op5=0) | 1 |
| not applicable: disguise: middleware/csrf_samesite_test.go uses a legacy // +build line | 1 |
| not applicable: heldout D7: assert/assertion_compare_test.go: Test_compareTwoValuesNotComparableValues has no t.Error/t.Fatal-style calls to demote | 2 |
| not applicable: heldout D7: assert/assertions_test.go: TestBytesEqual has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: assert/assertions_test.go: TestComparisonAssertionFunc has no t.Error/t.Fatal-style calls to demote | 2 |
| not applicable: heldout D7: auth_test.go: TestBasicAuthForProxy407 has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: auth_test.go: TestBasicAuthSucceed has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: bind_test.go: TestBindForm has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: binding/binding_test.go: TestBindingFormForType has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: context_test.go: TestContextClientIP has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: context_test.go: TestContextWithFallbackDeadlineFromRequestContext has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: context_test.go: TestContext_Logger has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: gin_test.go: TestEngineHandleContext has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: middleware/compress_test.go: TestGzipWithMinLengthTooShort has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: middleware/csrf_samesite_test.go: TestCSRFWithSameSiteModeNone has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: middleware/csrf_test.go: TestCSRF has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: middleware/proxy_test.go: TestModifyResponseUseContext has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: middleware/rate_limiter_test.go: TestRateLimiterMemoryStore_cleanupStaleVisitors has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: middleware/static_test.go: TestStatic has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: mock/mock_test.go: Test_Mock_UnsetByOnMethodSpecAmongOthers has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: heldout D7: render/render_test.go: TestRenderJsonpJSON has no t.Error/t.Fatal-style calls to demote | 1 |
| not applicable: test TestBasicWriterDiscardsWritesToOriginalResponseWriter has no assertion to turn into a log | 1 |
| not applicable: test TestBindMultipartForm has no assertion to turn into a log | 1 |
| not applicable: test TestBindingFormForType has no assertion to turn into a log | 1 |
| not applicable: test TestComparisonAssertionFunc has no assertion to turn into a log | 2 |
| not applicable: test TestContext_Logger has no assertion to turn into a log | 1 |
| not applicable: test TestTreeRunDynamicRouting has no assertion to turn into a log | 1 |
| not applicable: test TestTreeWildcard has no assertion to turn into a log | 1 |
| not applicable: test Test_compareTwoValuesNotComparableValues has no assertion to turn into a log | 2 |
| not applicable: test Test_containsValue has no assertion to turn into a log | 1 |
| static destructive: 1233 scanned, 25 kept | 1 |
| static destructive: 1478 scanned, 70 kept | 1 |
| static destructive: 503 scanned, 12 kept | 1 |
| static destructive: 538 scanned, 12 kept | 1 |
| static destructive: 836 scanned, 27 kept | 1 |
| static main: 1233 scanned, 284 kept | 1 |
| static main: 1478 scanned, 428 kept | 1 |
| static main: 503 scanned, 134 kept | 1 |
| static main: 538 scanned, 119 kept | 1 |
| static main: 836 scanned, 153 kept | 1 |
| tests fail at base | 78 |
| tests fail at commit | 3 |
| timed out at commit | 1 |
