"use strict";

const WEBSEARCH_BASE_URL_KEY = "searchroom.searchBaseUrl";
const WEBFETCH_BASE_URL_KEY = "searchroom.readBaseUrl";
const API_KEY_SESSION_KEY = "searchroom.apiKey";

const elements = Object.fromEntries([
  "searchForm", "queryInput", "apiKeyInput", "baseUrlInput", "providerInput", "regionInput", "limitInput",
  "includeDomainsInput", "excludeDomainsInput", "exactPhrasesInput", "anyTermsInput", "excludeTermsInput", "titleTermsInput", "fileTypesInput",
  "searchButton", "healthStatus",
  "searchStatus", "searchMeta", "searchNotices", "resultsList", "resultCount", "pagination",
  "previousPage", "nextPage", "pageLabel", "searchDiagnostics", "attemptCount", "diagnosticsBody",
  "searchJSONPanel", "searchJSONTree", "copySearchJSON",
  "readBaseUrlInput", "formatInput", "maxCharsInput", "readerState", "readerEmpty", "readerContent",
  "readerSite", "articleTitle", "articleUrl", "readerMeta", "readerNotices", "articleBody",
  "previewPanel", "markdownPanel", "readerJSONPanel", "readerJSONTree", "copyReaderJSON", "copyMarkdown",
  "debugPanel", "readerDiagnostics", "readerDiagnosticsBody"
].map((id) => [id, document.getElementById(id)]));

const state = {
  searchController: null,
  readController: null,
  selectedURL: "",
  selectedCard: null,
  selectedResult: null,
  searchPayload: null,
  readPayload: null,
  cursorHistory: [null],
  cursorIndex: 0
};

function defaultSearchBaseURL() { return "https://reporters-acceptable-silent-declined.trycloudflare.com"; }
function defaultReadBaseURL() { return "https://attempts-fixes-ana-broken.trycloudflare.com"; }

function normalizeBaseURL(value) {
  const normalized = String(value).trim().replace(/\/+$/, "");
  const url = new URL(normalized);
  if (!["http:", "https:"].includes(url.protocol)) throw new Error("API Base URL 只支持 http 或 https");
  return normalized;
}

function setHidden(element, hidden) {
  element.classList.toggle("is-hidden", hidden);
}

function clear(element) {
  element.replaceChildren();
}

function create(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined && text !== null) node.textContent = String(text);
  return node;
}

function prettyJSON(value) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function commaValues(element) {
  return element.value.split(",").map((value) => value.trim()).filter(Boolean);
}

async function copyText(button, value) {
  const original = button.textContent;
  try {
    await navigator.clipboard.writeText(value);
    button.textContent = "已复制";
  } catch {
    button.textContent = "复制失败";
  }
  window.setTimeout(() => { button.textContent = original; }, 1400);
}

function appendJSONNode(container, key, value, depth = 0) {
  const composite = value !== null && typeof value === "object";
  if (!composite) {
    const row = create("div", "json-leaf");
    if (key !== null) row.append(create("span", "json-key", `${key}: `));
    let type = typeof value;
    if (value === null) type = "null";
    const rendered = typeof value === "string" ? `"${value}"` : String(value);
    row.append(create("span", `json-value json-${type}`, rendered));
    container.append(row);
    return;
  }

  const isArray = Array.isArray(value);
  const entries = Object.entries(value);
  const details = document.createElement("details");
  details.open = depth < 2;
  const summary = document.createElement("summary");
  if (key !== null) summary.append(create("span", "json-key", `${key}: `));
  summary.append(document.createTextNode(`${isArray ? "Array" : "Object"}(${entries.length})`));
  details.append(summary);
  entries.forEach(([childKey, childValue]) => appendJSONNode(details, isArray ? childKey : childKey, childValue, depth + 1));
  container.append(details);
}

function renderJSONTree(container, payload) {
  clear(container);
  if (payload === null || payload === undefined) {
    container.append(create("div", "json-null", "没有 JSON 响应"));
    return;
  }
  appendJSONNode(container, null, payload);
}

function appendInlineMarkdown(container, source) {
  const pattern = /(`[^`]+`|\[[^\]]+\]\([^)]+\))/g;
  let cursor = 0;
  for (const match of source.matchAll(pattern)) {
    if (match.index > cursor) container.append(document.createTextNode(source.slice(cursor, match.index)));
    const token = match[0];
    if (token.startsWith("`")) {
      container.append(create("code", "", token.slice(1, -1)));
    } else {
      const parts = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(token);
      const label = parts?.[1] || token;
      const target = parts?.[2] || "";
      try {
        const url = new URL(target, window.location.href);
        if (!["http:", "https:", "mailto:"].includes(url.protocol)) throw new Error("unsafe protocol");
        const link = create("a", "", label);
        link.href = url.href;
        link.target = "_blank";
        link.rel = "noopener noreferrer";
        container.append(link);
      } catch {
        container.append(document.createTextNode(label));
      }
    }
    cursor = match.index + token.length;
  }
  if (cursor < source.length) container.append(document.createTextNode(source.slice(cursor)));
}

function renderMarkdown(container, source) {
  clear(container);
  const lines = String(source || "").replace(/\r\n?/g, "\n").split("\n");
  let index = 0;

  while (index < lines.length) {
    const line = lines[index];
    if (!line.trim()) { index += 1; continue; }

    if (line.startsWith("```")) {
      const codeLines = [];
      index += 1;
      while (index < lines.length && !lines[index].startsWith("```")) {
        codeLines.push(lines[index]);
        index += 1;
      }
      if (index < lines.length) index += 1;
      const pre = document.createElement("pre");
      pre.append(create("code", "", codeLines.join("\n")));
      container.append(pre);
      continue;
    }

    const heading = /^(#{1,6})\s+(.+)$/.exec(line);
    if (heading) {
      const node = document.createElement(`h${heading[1].length}`);
      appendInlineMarkdown(node, heading[2]);
      container.append(node);
      index += 1;
      continue;
    }

    if (/^>\s?/.test(line)) {
      const quoteLines = [];
      while (index < lines.length && /^>\s?/.test(lines[index])) {
        quoteLines.push(lines[index].replace(/^>\s?/, ""));
        index += 1;
      }
      const quote = document.createElement("blockquote");
      appendInlineMarkdown(quote, quoteLines.join(" "));
      container.append(quote);
      continue;
    }

    const unordered = /^[-*+]\s+(.+)$/.exec(line);
    const ordered = /^\d+[.)]\s+(.+)$/.exec(line);
    if (unordered || ordered) {
      const list = document.createElement(unordered ? "ul" : "ol");
      const matcher = unordered ? /^[-*+]\s+(.+)$/ : /^\d+[.)]\s+(.+)$/;
      while (index < lines.length) {
        const item = matcher.exec(lines[index]);
        if (!item) break;
        const node = document.createElement("li");
        appendInlineMarkdown(node, item[1]);
        list.append(node);
        index += 1;
      }
      container.append(list);
      continue;
    }

    const paragraphLines = [line.trim()];
    index += 1;
    while (index < lines.length && lines[index].trim() &&
      !/^(#{1,6})\s+/.test(lines[index]) && !lines[index].startsWith("```") &&
      !/^>\s?/.test(lines[index]) && !/^[-*+]\s+/.test(lines[index]) && !/^\d+[.)]\s+/.test(lines[index])) {
      paragraphLines.push(lines[index].trim());
      index += 1;
    }
    const paragraph = document.createElement("p");
    appendInlineMarkdown(paragraph, paragraphLines.join(" "));
    container.append(paragraph);
  }

  if (!container.childNodes.length) container.append(create("p", "", "正文为空"));
}

function activateReaderTab(name) {
  document.querySelectorAll("[data-reader-tab]").forEach((button) => {
    const active = button.dataset.readerTab === name;
    button.classList.toggle("is-active", active);
    button.setAttribute("aria-selected", String(active));
  });
  setHidden(elements.previewPanel, name !== "preview");
  setHidden(elements.markdownPanel, name !== "markdown");
  setHidden(elements.readerJSONPanel, name !== "json");
  setHidden(elements.debugPanel, name !== "debug");
}

function readable(value, fallback = "—") {
  return value === undefined || value === null || value === "" ? fallback : String(value);
}

function formatBoolean(value) {
  return value ? "是" : "否";
}

function hostFromURL(value) {
  try {
    return new URL(value).hostname;
  } catch {
    return value || "未知来源";
  }
}

function safeExternalURL(value) {
  try {
    const url = new URL(value);
    return ["http:", "https:"].includes(url.protocol) ? url.href : "#";
  } catch {
    return "#";
  }
}

function setHealth(kind, label) {
  elements.healthStatus.classList.remove("is-online", "is-error");
  if (kind) elements.healthStatus.classList.add(kind);
  elements.healthStatus.lastElementChild.textContent = label;
}

async function requestJSON(url, options = {}) {
  let response;
  try {
    response = await fetch(url, options);
  } catch (error) {
    const wrapped = new Error(error instanceof Error ? error.message : "网络请求失败");
    wrapped.status = 0;
    wrapped.payload = null;
    wrapped.rawText = "";
    throw wrapped;
  }

  const rawText = await response.text();
  let payload = null;
  if (rawText) {
    try {
      payload = JSON.parse(rawText);
    } catch {
      payload = null;
    }
  }

  if (!response.ok) {
    const message = payload?.detail || payload?.title || payload?.error?.message || payload?.message || rawText || `HTTP ${response.status}`;
    const error = new Error(message);
    error.status = response.status;
    error.payload = payload;
    error.rawText = rawText;
    throw error;
  }

  if (payload && typeof payload === "object" && Object.hasOwn(payload, "code")) {
    const code = Number(payload.code || 0);
    if (code !== 0) {
      const error = new Error(payload.message || `API Market error ${code}`);
      error.status = response.status;
      error.payload = payload;
      error.rawText = rawText;
      throw error;
    }
    payload = payload.response ?? {};
  }

  return { payload, status: response.status, rawText };
}

function apiMarketHeaders() {
  const apiKey = elements.apiKeyInput.value.trim();
  if (!apiKey) throw new Error("请输入 API Market Key");
  sessionStorage.setItem(API_KEY_SESSION_KEY, apiKey);
  return {
    "Authorization": `Bearer ${apiKey}`,
    "Content-Type": "application/json"
  };
}

function addMeta(container, label, value, accent = false) {
  const chip = create("span", `meta-chip${accent ? " is-accent" : ""}`);
  chip.append(create("span", "", `${label} ·`), create("strong", "", readable(value)));
  container.append(chip);
}

function renderWarnings(container, warnings) {
  clear(container);
  const items = Array.isArray(warnings) ? warnings : [];
  setHidden(container, items.length === 0);
  items.forEach((warning) => {
    const notice = create("div", "notice");
    notice.append(create("strong", "", readable(warning?.code, "warning")));
    notice.append(document.createTextNode(readable(warning?.message, "未提供说明")));
    container.append(notice);
  });
}

function addDiagnosticValue(list, label, value) {
  list.append(create("dt", "", label), create("dd", "", readable(value)));
}

function renderAttempts(container, debug, originalError, rawText) {
  clear(container);
  const attempts = Array.isArray(debug?.attempts) ? debug.attempts : [];

  if (originalError) {
    const block = create("section", "diagnostic-block");
    block.append(create("h4", "", "Original error"), create("pre", "error-raw", originalError));
    container.append(block);
  }

  attempts.forEach((attempt, index) => {
    const block = create("section", "diagnostic-block");
    block.append(create("h4", "", `Attempt ${index + 1} · ${readable(attempt.implementation || attempt.transport)}`));
    const list = create("dl");
    addDiagnosticValue(list, "Provider", attempt.provider);
    addDiagnosticValue(list, "Stage", attempt.stage);
    addDiagnosticValue(list, "Strategy", attempt.strategy);
    addDiagnosticValue(list, "Implementation", attempt.implementation);
    addDiagnosticValue(list, "Transport", attempt.transport);
    addDiagnosticValue(list, "Header profile", attempt.header_profile);
    addDiagnosticValue(list, "Classification", attempt.classification);
    addDiagnosticValue(list, "Session state", attempt.session_state);
    addDiagnosticValue(list, "Session generation", attempt.session_generation);
    addDiagnosticValue(list, "Session wait", `${readable(attempt.session_wait_ms, 0)} ms`);
    addDiagnosticValue(list, "Blocked until", attempt.blocked_until);
    addDiagnosticValue(list, "HTTP status", attempt.http_status);
    addDiagnosticValue(list, "Elapsed", `${readable(attempt.elapsed_ms, 0)} ms`);
    addDiagnosticValue(list, "Request URL", attempt.request_url);
    addDiagnosticValue(list, "Final URL", attempt.final_url);
    addDiagnosticValue(list, "Page title", attempt.page_title);
    addDiagnosticValue(list, "Parser error", attempt.parser_error);
    addDiagnosticValue(list, "Original error", attempt.original_error);
    addDiagnosticValue(list, "Body SHA-256", attempt.body_sha256);
    block.append(list);
    if (attempt.body_preview) block.append(create("pre", "", attempt.body_preview));
    container.append(block);
  });

  const artifacts = Array.isArray(debug?.raw_artifacts) ? debug.raw_artifacts : [];
  if (artifacts.length) {
    const block = create("section", "diagnostic-block");
    block.append(create("h4", "", "Artifacts"), create("pre", "", artifacts.join("\n")));
    container.append(block);
  }

  if (rawText && !debug && !originalError) {
    const block = create("section", "diagnostic-block");
    block.append(create("h4", "", "Raw response"), create("pre", "", rawText));
    container.append(block);
  }

  return attempts.length;
}

function showSearchStatus(kind, title, description, rawDetails) {
  elements.searchStatus.className = `status-card ${kind === "loading" ? "is-loading" : kind === "error" ? "is-error" : "empty-state"}`;
  clear(elements.searchStatus);
  elements.searchStatus.append(create("span", "status-index", kind === "error" ? "!" : kind === "loading" ? "…" : "01"));
  const copy = create("div");
  copy.append(create("strong", "", title), create("p", "", description));
  if (rawDetails) copy.append(create("pre", "error-raw", rawDetails));
  elements.searchStatus.append(copy);
  setHidden(elements.searchStatus, false);
}

function renderSearchMeta(response) {
  clear(elements.searchMeta);
  const meta = response?.meta || {};
  addMeta(elements.searchMeta, "Actual", meta.provider || response?.provider || response?.selected_provider, true);
  addMeta(elements.searchMeta, "Requested", meta.requested_provider || response?.requested_provider || elements.providerInput.value);
  if (meta.strategy) addMeta(elements.searchMeta, "Strategy", meta.strategy);
  addMeta(elements.searchMeta, "Transport", meta.transport);
  addMeta(elements.searchMeta, "Time", `${readable(meta.took_ms, 0)} ms`);
  if (meta.search_took_ms !== undefined) addMeta(elements.searchMeta, "Search", `${meta.search_took_ms} ms`);
  if (meta.read_took_ms !== undefined) addMeta(elements.searchMeta, "Read", `${meta.read_took_ms} ms`);
  if (response?.candidate_count !== undefined) addMeta(elements.searchMeta, "Candidates", response.candidate_count);
  if (response?.readable_count !== undefined) addMeta(elements.searchMeta, "Readable", response.readable_count);
  addMeta(elements.searchMeta, "Cache", meta.cached ? `${readable(meta.cache_age_seconds, 0)}s` : "miss");
  addMeta(elements.searchMeta, "Fallback", meta.provider_fallback_count ?? meta.fallback_count ?? 0);
  addMeta(elements.searchMeta, "Degraded", formatBoolean(meta.degraded));
  addMeta(elements.searchMeta, "Request", response?.request_id);
  setHidden(elements.searchMeta, false);
}

function createResultCard(result, index, fallbackProvider) {
  const card = create("button", "result-card");
  card.type = "button";
  card.dataset.url = result?.url || "";
  card.setAttribute("aria-label", `读取正文：${readable(result?.title, "无标题")}`);
  const rank = create("span", "rank");
  const selectedRank = result?.selected_rank;
  const originalRank = result?.original_rank ?? result?.rank ?? index + 1;
  rank.append(create("span", "rank-primary", String(selectedRank ?? originalRank).padStart(2, "0")));
  if (selectedRank !== undefined) rank.append(create("small", "rank-detail", `原 ${originalRank}`));
  card.append(rank);
  const main = create("span", "result-main");
  main.append(
    create("span", "result-source", readable(result?.provider, fallbackProvider)),
    create("h3", "", readable(result?.title, "无标题")),
    create("p", "", readable(result?.snippet, "没有摘要")),
    create("span", "result-host", hostFromURL(result?.url))
  );
  card.append(main, create("span", "result-arrow", "↗"));
  card.addEventListener("click", () => selectAndRead(card, result));
  return card;
}

function renderSearchResponse(response) {
  state.searchPayload = response;
  const results = Array.isArray(response?.results) ? response.results : [];
  clear(elements.resultsList);
  setHidden(elements.searchStatus, results.length > 0);
  if (!results.length) showSearchStatus("empty", "查询完成，但没有结果", "可以更换关键词、Provider 或页码后重试。");
  const fallbackProvider = response?.provider || response?.selected_provider;
  results.forEach((result, index) => elements.resultsList.append(createResultCard(result, index, fallbackProvider)));

  elements.resultCount.textContent = `${results.length} 条 · ${readable(response?.query, "")}`;
  renderSearchMeta(response);
  const failureWarnings = (Array.isArray(response?.failures) ? response.failures : []).map((failure) => ({
    code: failure?.code || "read_failed",
    message: `原排名 ${readable(failure?.original_rank)} · ${readable(failure?.url)}`
  }));
  renderWarnings(elements.searchNotices, [...(response?.warnings || []), ...failureWarnings]);

  const attemptTotal = renderAttempts(elements.diagnosticsBody, response?.debug, "", "");
  elements.attemptCount.textContent = attemptTotal ? `· ${attemptTotal} attempts` : "";
  setHidden(elements.searchDiagnostics, !response?.debug);
  renderJSONTree(elements.searchJSONTree, response);
  setHidden(elements.searchJSONPanel, false);

  const nextCursor = response?.page?.next_cursor || null;
  state.cursorHistory[state.cursorIndex + 1] = nextCursor;
  elements.pageLabel.textContent = `第 ${state.cursorIndex + 1} 页`;
  elements.previousPage.disabled = state.cursorIndex === 0;
  elements.nextPage.disabled = !response?.page?.has_more || !nextCursor;
  setHidden(elements.pagination, results.length === 0);
}

function renderSearchError(error) {
  const payload = error.payload || {};
  const apiError = payload.error || payload;
  const details = [
    `HTTP: ${error.status || "network"}`,
    apiError.code ? `code: ${apiError.code}` : "",
    apiError.retryable !== undefined ? `retryable: ${apiError.retryable}` : "",
    payload.request_id ? `request_id: ${payload.request_id}` : "",
    apiError.original_error ? `original_error: ${apiError.original_error}` : "",
    !payload.error && error.rawText ? error.rawText : ""
  ].filter(Boolean).join("\n");

  showSearchStatus("error", readable(apiError.detail || apiError.message, error.message), readable(apiError.code, `HTTP ${error.status || 0}`), details);
  elements.resultCount.textContent = "查询失败";
  clear(elements.resultsList);
  clear(elements.searchMeta);
  setHidden(elements.searchMeta, true);
  renderWarnings(elements.searchNotices, payload.warnings);
  const attemptTotal = renderAttempts(elements.diagnosticsBody, payload.debug, apiError.original_error, !payload.error ? error.rawText : "");
  elements.attemptCount.textContent = attemptTotal ? `· ${attemptTotal} attempts` : "";
  setHidden(elements.searchDiagnostics, !(payload.debug || apiError.original_error || error.rawText));
  state.searchPayload = payload.code || payload.error ? payload : { http_status: error.status || 0, raw_response: error.rawText || error.message };
  renderJSONTree(elements.searchJSONTree, state.searchPayload);
  setHidden(elements.searchJSONPanel, false);
  setHidden(elements.pagination, true);
}

async function runSearch() {
  const query = elements.queryInput.value.trim();
  if (!query) {
    elements.queryInput.focus();
    return;
  }

  state.searchController?.abort();
  state.searchController = new AbortController();
  let baseURL;
  try {
    baseURL = normalizeBaseURL(elements.baseUrlInput.value);
  } catch (error) {
    renderSearchError(Object.assign(error, { status: 0, payload: null, rawText: "" }));
    setHealth("is-error", "Base URL 无效");
    return;
  }
  elements.baseUrlInput.value = baseURL;
  sessionStorage.setItem(WEBSEARCH_BASE_URL_KEY, baseURL);
  const region = elements.regionInput.value.trim();

  showSearchStatus("loading", "正在查询搜索源", "Provider 路由、缓存与响应元数据会在完成后呈现。");
  clear(elements.resultsList);
  setHidden(elements.searchMeta, true);
  setHidden(elements.searchNotices, true);
  setHidden(elements.searchDiagnostics, true);
  setHidden(elements.searchJSONPanel, true);
  setHidden(elements.pagination, true);
  elements.resultCount.textContent = "查询中…";
  elements.searchButton.disabled = true;
  setHealth("", "正在请求");

  try {
    const body = {
      query,
      limit: Number(elements.limitInput.value) || 10,
      timeout: "20s",
      routing: {
        providers: elements.providerInput.value === "auto"
          ? ["baidu", "bing", "brave", "duckduckgo"]
          : [elements.providerInput.value]
      }
    };
    const filters = {
      include_domains: commaValues(elements.includeDomainsInput),
      exclude_domains: commaValues(elements.excludeDomainsInput)
    };
    if (region) filters.region = region;
    if (filters.region || filters.include_domains.length || filters.exclude_domains.length) body.filters = filters;
    const queryOptions = {
      exact_phrases: commaValues(elements.exactPhrasesInput),
      any_terms: commaValues(elements.anyTermsInput),
      exclude_terms: commaValues(elements.excludeTermsInput),
      title_terms: commaValues(elements.titleTermsInput),
      file_types: commaValues(elements.fileTypesInput)
    };
    if (Object.values(queryOptions).some((values) => values.length)) body.query_options = queryOptions;
    const cursor = state.cursorHistory[state.cursorIndex];
    if (cursor) body.cursor = cursor;
    const { payload } = await requestJSON(`${baseURL}/v1/websearch`, {
      method: "POST",
      headers: apiMarketHeaders(),
      body: JSON.stringify(body),
      signal: state.searchController.signal
    });
    renderSearchResponse(payload || {});
    setHealth("is-online", "API 已连接");
  } catch (error) {
    if (error.name === "AbortError") return;
    renderSearchError(error);
    setHealth("is-error", "API 请求失败");
  } finally {
    elements.searchButton.disabled = false;
  }
}

function setReaderState(kind, label) {
  elements.readerState.className = `reader-state${kind ? ` ${kind}` : ""}`;
  elements.readerState.textContent = label;
}

function showReaderError(error, selected) {
  const payload = error.payload || {};
  const apiError = payload.error || payload;
  setHidden(elements.readerEmpty, true);
  setHidden(elements.readerContent, false);
  elements.readerSite.textContent = hostFromURL(selected?.url);
  elements.articleTitle.textContent = readable(selected?.title, "正文读取失败");
  elements.articleUrl.href = safeExternalURL(selected?.url);
  clear(elements.readerMeta);
  addMeta(elements.readerMeta, "HTTP", error.status || "network", true);
  addMeta(elements.readerMeta, "Code", apiError.code);
  addMeta(elements.readerMeta, "Retryable", apiError.retryable === undefined ? "—" : formatBoolean(apiError.retryable));
  addMeta(elements.readerMeta, "Request", payload.request_id);
  renderWarnings(elements.readerNotices, payload.warnings);
  elements.articleBody.textContent = [
    readable(apiError.detail || apiError.message, error.message),
    apiError.original_error ? `\nOriginal error:\n${apiError.original_error}` : "",
    !payload.error && error.rawText ? `\nRaw response:\n${error.rawText}` : ""
  ].filter(Boolean).join("\n");
  renderMarkdown(elements.previewPanel, elements.articleBody.textContent);
  state.readPayload = payload.code || payload.error ? payload : { http_status: error.status || 0, raw_response: error.rawText || error.message };
  renderJSONTree(elements.readerJSONTree, state.readPayload);
  renderAttempts(elements.readerDiagnosticsBody, payload.debug, apiError.original_error, !payload.error ? error.rawText : "");
  setHidden(elements.readerDiagnostics, !(payload.debug || apiError.original_error || error.rawText));
  setReaderState("is-error", "读取失败");
  activateReaderTab("preview");
}

function renderReadResponse(response) {
  state.readPayload = response;
  const document = response?.document || {};
  setHidden(elements.readerEmpty, true);
  setHidden(elements.readerContent, false);
  elements.readerSite.textContent = hostFromURL(document.final_url || document.url);
  elements.articleTitle.textContent = readable(document.title, "无标题正文");
  elements.articleUrl.href = safeExternalURL(document.final_url || document.url);
  elements.articleBody.textContent = readable(document.content, "正文为空");
  renderMarkdown(elements.previewPanel, document.content);
  renderJSONTree(elements.readerJSONTree, response);

  clear(elements.readerMeta);
  const meta = response?.meta || {};
  addMeta(elements.readerMeta, "Format", document.format, true);
  addMeta(elements.readerMeta, "Content-Type", document.content_type);
  addMeta(elements.readerMeta, "Status", document.status_code);
  addMeta(elements.readerMeta, "Length", meta.content_length);
  addMeta(elements.readerMeta, "Transport", meta.transport);
  addMeta(elements.readerMeta, "Time", `${readable(meta.took_ms, 0)} ms`);
  addMeta(elements.readerMeta, "Cached", formatBoolean(meta.cached));
  addMeta(elements.readerMeta, "Truncated", formatBoolean(meta.truncated));
  addMeta(elements.readerMeta, "Request", response?.request_id);
  if (document.author) addMeta(elements.readerMeta, "Author", document.author);
  if (document.published_at) addMeta(elements.readerMeta, "Published", document.published_at);
  if (document.language) addMeta(elements.readerMeta, "Language", document.language);

  renderWarnings(elements.readerNotices, response?.warnings);
  renderAttempts(elements.readerDiagnosticsBody, response?.debug, "", "");
  setHidden(elements.readerDiagnostics, !response?.debug);
  setReaderState("is-ready", meta.truncated ? "已读取 · 截断" : "已读取");
  activateReaderTab("preview");
}

async function readResult(result) {
  if (!result?.url) return;
  state.readController?.abort();
  state.readController = new AbortController();
  const body = {
    url: result.url,
    timeout: "20s",
    output: {
      format: elements.formatInput.value,
      max_chars: Number(elements.maxCharsInput.value) || 30000
    }
  };

  setHidden(elements.readerEmpty, true);
  setHidden(elements.readerContent, false);
  elements.readerSite.textContent = hostFromURL(result.url);
  elements.articleTitle.textContent = readable(result.title, "读取正文");
  elements.articleUrl.href = safeExternalURL(result.url);
  elements.articleBody.textContent = "正在获取、提取并转换正文…";
  renderMarkdown(elements.previewPanel, "正在获取、提取并转换正文…");
  renderJSONTree(elements.readerJSONTree, null);
  clear(elements.readerMeta);
  clear(elements.readerNotices);
  setHidden(elements.readerNotices, true);
  setHidden(elements.readerDiagnostics, true);
  setReaderState("is-loading", "读取中…");
  activateReaderTab("preview");

  try {
    const readBaseURL = normalizeBaseURL(elements.readBaseUrlInput.value);
    elements.readBaseUrlInput.value = readBaseURL;
    sessionStorage.setItem(WEBFETCH_BASE_URL_KEY, readBaseURL);
    const { payload } = await requestJSON(`${readBaseURL}/v1/webfetch`, {
      method: "POST",
      headers: apiMarketHeaders(),
      body: JSON.stringify(body),
      signal: state.readController.signal
    });
    renderReadResponse(payload || {});
  } catch (error) {
    if (error.name === "AbortError") return;
    showReaderError(error, result);
  }
}

function selectAndRead(card, result) {
  state.selectedCard?.classList.remove("is-selected");
  state.selectedCard = card;
  state.selectedURL = result?.url || "";
  state.selectedResult = result;
  card.classList.add("is-selected");
  readResult(result);
  if (window.innerWidth <= 1080) {
    document.querySelector(".reader-column").scrollIntoView({ behavior: "smooth", block: "start" });
  }
}

function changePage(delta) {
  const next = Math.max(0, state.cursorIndex + delta);
  if (next > 0 && !state.cursorHistory[next]) return;
  state.cursorIndex = next;
  runSearch();
}

function initialize() {
  elements.baseUrlInput.value = sessionStorage.getItem(WEBSEARCH_BASE_URL_KEY) || defaultSearchBaseURL();
  elements.readBaseUrlInput.value = sessionStorage.getItem(WEBFETCH_BASE_URL_KEY) || defaultReadBaseURL();
  elements.apiKeyInput.value = sessionStorage.getItem(API_KEY_SESSION_KEY) || "";

  elements.searchForm.addEventListener("submit", (event) => {
    event.preventDefault();
    state.cursorHistory = [null];
    state.cursorIndex = 0;
    runSearch();
  });
  elements.previousPage.addEventListener("click", () => changePage(-1));
  elements.nextPage.addEventListener("click", () => changePage(1));
  document.querySelectorAll("[data-reader-tab]").forEach((button) => {
    button.addEventListener("click", () => activateReaderTab(button.dataset.readerTab));
  });
  elements.copySearchJSON.addEventListener("click", () => copyText(elements.copySearchJSON, prettyJSON(state.searchPayload)));
  elements.copyReaderJSON.addEventListener("click", () => copyText(elements.copyReaderJSON, prettyJSON(state.readPayload)));
  elements.copyMarkdown.addEventListener("click", () => copyText(elements.copyMarkdown, elements.articleBody.textContent));
  elements.formatInput.addEventListener("change", () => {
    if (state.selectedURL && state.selectedCard) {
      readResult({ url: state.selectedURL, title: elements.articleTitle.textContent });
    }
  });
}

initialize();
