# Parity-Plus Search Benchmark Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compare this backend fairly against a fixed `web-search-mcp` baseline and fail the benchmark whenever any approved parity-plus gate is missed.

**Architecture:** Extend the existing comparison demo with two adapters that emit one normalized run record: MCP `full-web-search` and backend `POST /v1/search` with content enabled. A deterministic runner alternates system order, preserves raw output, computes grouped metrics, and generates machine-readable and Markdown reports.

**Tech Stack:** Node.js ESM already used by `web-search-comparison-demo`, MCP stdio client, native `fetch`, JSON/Markdown artifacts.

## Global Constraints

- Baseline is `mrkrsl/web-search-mcp` commit `eeb03f88525cbf74c4019e59a3fea45a537a760b`, package version 0.3.1.
- Call MCP `full-web-search`; do not substitute summary search plus one page read.
- Use 20 versioned queries: 14 Chinese and 6 English.
- Request 5 usable bodies per query, maximum 30000 Unicode characters each, with a 30-second total budget.
- Run at least 3 rounds per system and alternate system order deterministically.
- Keep CAPTCHA, timeout, empty-result, and missing-body failures in denominators.
- Preserve raw JSON, normalized JSON, summary JSON, and Markdown report for every benchmark version.
- Mark the report `FAIL` if any of the eight gates in the design is missed.

---

### Task 1: Versioned query corpus and normalized run schema

**Files:**
- Create: `/Users/zoe/Documents/daily/web-search-comparison-demo/benchmarks/parity-plus/v1/queries.json`
- Create: `/Users/zoe/Documents/daily/web-search-comparison-demo/scripts/parity-plus/schema.mjs`
- Test: `/Users/zoe/Documents/daily/web-search-comparison-demo/scripts/parity-plus/schema.test.mjs`

**Interfaces:**
- Produces: `validateCorpus`, `validateRunRecord`, and a fixed corpus with language, category, intent terms, synonyms, and irrelevant patterns.

- [ ] **Step 1: Write failing corpus validation tests**

```js
test('corpus contains exactly 14 Chinese and 6 English queries', async () => {
  const corpus = JSON.parse(await readFile(corpusPath, 'utf8'));
  validateCorpus(corpus);
  assert.equal(corpus.filter((q) => q.language === 'zh').length, 14);
  assert.equal(corpus.filter((q) => q.language === 'en').length, 6);
});
```

- [ ] **Step 2: Run `node --test scripts/parity-plus/schema.test.mjs` and verify failure**

Expected: FAIL because the schema module and corpus do not exist.

- [ ] **Step 3: Add the complete fixed corpus and strict validators**

Each query object must include `id`, `query`, `language`, `category`, non-empty `intent_terms`, `synonyms`, and `irrelevant_patterns`. Reject duplicate IDs, ad hoc query replacement, missing bodies, and unknown run statuses.

- [ ] **Step 4: Run the schema test**

Run: `node --test scripts/parity-plus/schema.test.mjs`

Expected: PASS.

- [ ] **Step 5: Commit the corpus in the comparison repository**

```bash
git add benchmarks/parity-plus/v1/queries.json scripts/parity-plus/schema.mjs scripts/parity-plus/schema.test.mjs
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "test: add parity-plus query corpus"
```

### Task 2: Fair MCP and backend adapters

**Files:**
- Create: `/Users/zoe/Documents/daily/web-search-comparison-demo/scripts/parity-plus/mcp-adapter.mjs`
- Create: `/Users/zoe/Documents/daily/web-search-comparison-demo/scripts/parity-plus/backend-adapter.mjs`
- Test: `/Users/zoe/Documents/daily/web-search-comparison-demo/scripts/parity-plus/adapters.test.mjs`
- Modify: `/Users/zoe/Documents/daily/web-search-comparison-demo/scripts/test-web-search-mcp.mjs`

**Interfaces:**
- Produces: `runMCP(query, options)` and `runBackend(query, options)`, both returning the same normalized record.

- [ ] **Step 1: Write failing adapter contract tests with fixture transports**

```js
test('both adapters preserve five-body target and rank metadata', async () => {
  const backend = await runBackend('go', { limit: 5, fetchImpl: backendFixtureFetch });
  const mcp = await runMCP('go', { limit: 5, callTool: mcpFixtureCall });
  for (const record of [backend, mcp]) {
    assert.equal(record.target_count, 5);
    assert.ok(Array.isArray(record.results));
    assert.ok(record.results.every((item) => Number.isInteger(item.original_rank)));
  }
});
```

- [ ] **Step 2: Run adapter tests and verify failure**

Run: `node --test scripts/parity-plus/adapters.test.mjs`

Expected: FAIL because the adapters do not exist.

- [ ] **Step 3: Implement identical budgets and normalized failure capture**

Call MCP `full-web-search` with `limit=5`. Call backend `POST /v1/search` with `limit=5`, `refresh=true`, and `content.enabled=true`. Abort each at 30 seconds. Preserve original raw response, Provider/engine, ranks, bodies, error codes, CAPTCHA, timeout, and elapsed time; never discard a failed candidate from the metrics input.

- [ ] **Step 4: Run adapter tests**

Run: `node --test scripts/parity-plus/adapters.test.mjs`

Expected: PASS.

- [ ] **Step 5: Commit adapter support**

```bash
git add scripts/parity-plus scripts/test-web-search-mcp.mjs
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "test: add fair search system adapters"
```

### Task 3: Runner, metrics, gates, and report

**Files:**
- Create: `/Users/zoe/Documents/daily/web-search-comparison-demo/scripts/parity-plus/run.mjs`
- Create: `/Users/zoe/Documents/daily/web-search-comparison-demo/scripts/parity-plus/metrics.mjs`
- Create: `/Users/zoe/Documents/daily/web-search-comparison-demo/scripts/parity-plus/report.mjs`
- Test: `/Users/zoe/Documents/daily/web-search-comparison-demo/scripts/parity-plus/metrics.test.mjs`
- Modify: `/Users/zoe/Documents/daily/web-search-comparison-demo/package.json`

**Interfaces:**
- Produces: `npm run benchmark:parity-plus`, summary metrics, per-query gaps, all eight gate results, and a final `PASS` or `FAIL`.

- [ ] **Step 1: Write failing formula and gate tests**

```js
test('fulfillment keeps failures in the denominator', () => {
  const runs = [{ target_count: 5, readable_count: 3 }, { target_count: 5, readable_count: 0 }];
  assert.equal(fulfillment(runs), 0.3);
});

test('report fails when one mandatory gate fails', () => {
  const result = evaluateGates(candidateFixture, baselineFixture);
  assert.equal(result.status, 'FAIL');
  assert.ok(result.gates.some((gate) => gate.pass === false));
});
```

- [ ] **Step 2: Run metrics tests and verify failure**

Run: `node --test scripts/parity-plus/metrics.test.mjs`

Expected: FAIL because the metric functions do not exist.

- [ ] **Step 3: Implement deterministic scheduling and all approved metrics**

Alternate system order by `(round + queryIndex) % 2`, wait at least two seconds between queries, and write timestamped raw records without overwriting history. Compute overall and language-group search success, readable fulfillment, relevance, intent coverage, unrelated ratio, unique domains, duplicate URLs, CAPTCHA ratio, P50/P95 latency, and blind-pairwise records that require explicit ratings before gate 5 can pass.

- [ ] **Step 4: Run all comparison-demo tests**

Run: `npm test`

Expected: PASS.

- [ ] **Step 5: Commit benchmark orchestration**

```bash
git add scripts/parity-plus package.json
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "test: automate parity-plus benchmark"
```

### Task 4: Execute three rounds and audit the result

**Files:**
- Create: `benchmarks/parity-plus/v1/runs/YYYYMMDD-HHMMSS/raw.jsonl` under the comparison repository, where the directory name is generated once from the UTC run start.
- Create: `benchmarks/parity-plus/v1/runs/YYYYMMDD-HHMMSS/summary.json` under the same generated run directory.
- Create: `benchmarks/parity-plus/v1/runs/YYYYMMDD-HHMMSS/report.md` under the same generated run directory.
- Create: `benchmarks/parity-plus/v1/runs/YYYYMMDD-HHMMSS/environment.json` under the same generated run directory.

**Interfaces:**
- Consumes: running backend and fixed MCP baseline.
- Produces: at least 60 runs per system and an auditable gate report.

- [ ] **Step 1: Record immutable environment metadata**

Record both Git commits, MCP package version, Node and Go versions, query-corpus SHA-256, network/proxy state, start time, cache policy, timeouts, and system-order seed.

- [ ] **Step 2: Run the benchmark**

Run: `BACKEND_URL=http://127.0.0.1:8080 BENCHMARK_ROUNDS=3 npm run benchmark:parity-plus`

Expected: exactly 60 normalized runs for Candidate and 60 for Baseline, with every raw response retained.

- [ ] **Step 3: Complete blind pairwise ratings**

Use the generated anonymized result-set file, record one of `win`, `tie`, or `lose` for every sampled query pair, and regenerate the report. Do not reveal system identity until ratings are complete.

- [ ] **Step 4: Audit denominators and gate status**

Confirm failures remain in denominators, every Candidate result has Provider and both ranks, and every gate has evidence. If the report is `FAIL`, use its per-query gaps to drive another backend TDD iteration and rerun the full benchmark rather than deleting difficult queries.

- [ ] **Step 5: Commit benchmark evidence without pushing**

```bash
git add benchmarks/parity-plus/v1/runs
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "test: record parity-plus benchmark evidence"
```
