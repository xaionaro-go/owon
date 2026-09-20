const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

const appSource = fs.readFileSync(`${__dirname}/ui/app.js`, "utf8");

function createElement(overrides = {}) {
  const listeners = new Map();
  const element = {
    className: "",
    checked: false,
    dataset: {},
    disabled: false,
    elements: [],
    name: "",
    textContent: "",
    type: "",
    value: "",
    addEventListener(type, listener) {
      const callbacks = listeners.get(type) || [];
      callbacks.push(listener);
      listeners.set(type, callbacks);
    },
    dispatch(type, event = {}) {
      const dispatched = { target: element, ...event };
      for (const listener of listeners.get(type) || []) listener(dispatched);
      return dispatched;
    },
    children: [],
    append(...children) { this.children.push(...children); },
    replaceChildren(...children) { this.children = children; },
    setAttribute(name, value) { this[name] = String(value); },
    removeAttribute(name) { delete this[name]; },
    getAttribute(name) { return this[name] ?? null; },
    focus(options) { this.focused = true; this.focusOptions = options; this.focusCount = (this.focusCount || 0) + 1; },
    scrollIntoView(options) { this.scrollOptions = options; this.scrollCount = (this.scrollCount || 0) + 1; },
    click() { this.clicked = true; },
    hasAttribute(attribute) {
      if (attribute === "data-optional") return this.dataset.optional !== undefined;
      if (attribute === "data-fixed-value") return this.dataset.fixedValue !== undefined;
      return this[attribute] !== undefined;
    },
    matches(selector) { return selector === "[data-optional]" && this.dataset.optional !== undefined; },
    classList: {
      toggle() {},
    },
    ...overrides,
  };
  return element;
}

function createDocument() {
  const ids = new Map();
  const documentListeners = new Map();
  const toastMessages = [];
  for (const id of [
    "toast", "connection-banner", "connection-label", "connection-dot", "footer-status",
    "hydration-status", "device-model", "device-meta", "capability-list", "state-json",
    "last-update", "metric-ch1", "metric-ch1-unit", "metric-ch2", "metric-ch2-unit",
    "metric-acquisition", "metric-trigger", "dmm-range-state", "event-sequence",
    "event-gaps", "waveform-gaps", "event-retries", "stream-interval", "stream-queue",
    "stream-controls", "stream-header", "stream-wave-ch1", "stream-wave-ch2", "stream-apply",
    "download-command", "download-waveform", "inspect-header", "waveform-result",
    "waveform-header", "download-trace-csv", "download-trace-svg", "command-result", "metric-dmm", "dmm-hold", "dmm-current-note", "dmm-status", "dmm-current-error", "dmm-current-field",
    "observed-ch1", "observed-ch2", "observed-acquisition", "observed-horizontal", "observed-trigger", "measurements",
    "scope-plots", "scope-cursor-panels", "scope-ch1-status", "scope-ch2-status", "scope-notice", "raw-source", "inspect-waveform",
  ]) ids.set(id, createElement());
  const toast = ids.get("toast");
  let toastText = "";
  Object.defineProperty(toast, "textContent", {
    get() { return toastText; },
    set(value) {
      toastText = value;
      if (value) toastMessages.push(value);
    },
  });
  Object.assign(ids.get("stream-interval"), { value: "1000", defaultValue: "1000", min: "20", max: "3600000" });
  Object.assign(ids.get("stream-queue"), { value: "128", defaultValue: "128", min: "1", max: "1024" });
  ids.get("stream-controls").checked = true;
  ids.get("stream-wave-ch1").checked = true;
  ids.get("stream-wave-ch2").checked = true;
  ids.get("raw-source").value = "CHANNEL_1";
  ids.get("device-meta").textContent = "Waiting for daemon identity";

  const submit = createElement({ type: "submit" });
  const channel = createElement({ name: "channel", value: "CHANNEL_1" });
  const display = createElement({ name: "display", type: "checkbox", checked: false, dataset: { optional: "" } });
  const offset = createElement({ name: "offsetDivisions", type: "number", min: "-200", max: "200", value: "0", dataset: { optional: "" } });
  const scale = createElement({ name: "scale", value: "500mV", dataset: { optional: "" } });
  const channelForm = createElement({
    dataset: { api: "/api/channel", toast: "Channel applied" },
    elements: [channel, display, offset, scale, submit],
    querySelector(selector) { return selector === "button[type=submit]" ? submit : null; },
  });
  const dmmSubmit = createElement({ type: "submit" });
  const relative = createElement({ name: "relative", type: "checkbox", checked: false, dataset: { optional: "" } });
  const autoRange = createElement({ name: "autoRange", type: "checkbox", checked: true, disabled: true, dataset: { fixedValue: "true" } });
  const dmmFunction = createElement({ name: "function", dataset: { optional: "" } });
  const currentType = createElement({ name: "currentType", dataset: { optional: "" } });
  const range = createElement({ name: "range", dataset: { optional: "" } });
  const dmmForm = createElement({
    dataset: { api: "/api/dmm", toast: "DMM applied" },
    elements: [dmmFunction, currentType, range, relative, autoRange, dmmSubmit],
    querySelector(selector) { return selector === "button[type=submit]" ? dmmSubmit : null; },
  });
  const measurement = createElement({ dataset: { measurement: "CHANNEL_1:FREQUENCY" }, checked: true });
  const dmmRead = createElement({ dataset: { get: "/api/dmm/measurement" } });
  const autoAction = createElement({ dataset: { action: "/api/auto", resultSemantics: "no-response-unverified" } });
  const dmmHold = ids.get("dmm-hold");
  const horizontalOffset = createElement({ name: "offsetDivisions", type: "number", value: "", dataset: { optional: "" } });
  const horizontalSubmit = createElement({ type: "submit" });
  const horizontalForm = createElement({
    dataset: { api: "/api/horizontal" },
    elements: [horizontalOffset, horizontalSubmit],
    querySelector() { return horizontalSubmit; },
  });
  const level = createElement({ name: "levelVolts", type: "number", value: "0.25", dataset: { optional: "" } });
  const triggerSubmit = createElement({ type: "submit" });
  const triggerForm = createElement({
    dataset: { api: "/api/trigger" },
    elements: [level, triggerSubmit],
    querySelector() { return triggerSubmit; },
  });
  const waveform = createElement({ name: "waveform", dataset: { optional: "" } });
  const frequencyHz = createElement({ name: "frequencyHz", type: "number", dataset: { optional: "" } });
  const load = createElement({ name: "load", dataset: { optional: "" } });
  const output = createElement({ name: "output", type: "checkbox", dataset: { optional: "" } });
  const symmetryPercent = createElement({ name: "symmetryPercent", type: "number", dataset: { optional: "" } });
  const generatorNumbers = ["periodSeconds", "amplitudeVolts", "offsetVolts", "highVolts", "lowVolts", "dutyPercent", "pulseWidthSeconds", "risingSeconds", "fallingSeconds"].map(name => createElement({ name, type: "number", dataset: { optional: "" } }));
  const generatorSubmit = createElement({ type: "submit" });
  const generatorForm = createElement({
    dataset: { api: "/api/generator" },
    elements: [waveform, frequencyHz, load, output, symmetryPercent, ...generatorNumbers, generatorSubmit],
    querySelector() { return generatorSubmit; },
  });
  for (const input of generatorForm.elements.filter(input => input.name)) {
    for (const suffix of ["field", "error", "pending"]) ids.set(`generator-${input.name}-${suffix}`, createElement());
  }
  const captureSubmit = createElement({ type: "submit" });
  const captureForm = createElement({ dataset: { api: "/api/waveform" }, elements: [createElement({ name: "channel", value: "CHANNEL_1" }), createElement({ name: "screen", dataset: { fixedValue: "true" } }), captureSubmit], querySelector() { return captureSubmit; } });
  const forms = [channelForm, dmmForm, horizontalForm, triggerForm, generatorForm, captureForm];
  for (const form of forms) {
    const submitButton = form.querySelector("button[type=submit]");
    form.feedback = createElement();
    form.querySelector = selector => selector === "[data-form-status]" ? form.feedback : selector === "button[type=submit]" ? submitButton : null;
    for (const input of form.elements) {
      input.form = form;
      if (input.name) form.elements[input.name] = input;
    }
  }

  return {
    createElement(name) { return createElement({ tagName: name }); },
    createElementNS(namespace, name) { return createElement({ tagName: name }); },
    addEventListener(type, listener) {
      documentListeners.set(type, listener);
    },
    dispatch(type, event = {}) {
      const listener = documentListeners.get(type);
      if (listener) listener({ target: event.target || null, ...event });
    },
    querySelector(selector) {
      if (selector === "[data-get]") return dmmRead;
      if (selector === 'form[data-api="/api/dmm"]') return dmmForm;
      if (selector === 'form[data-api="/api/generator"]') return generatorForm;
      if (selector.startsWith("#")) return ids.get(selector.slice(1)) || null;
      return null;
    },
    querySelectorAll(selector) {
      if (selector === "[data-get]") return [dmmRead];
      if (selector === "form[data-api]") return forms;
      if (selector === "[data-measurement]:checked") return measurement.checked ? [measurement] : [];
      if (selector === "[data-action]") return [autoAction];
      return [];
    },
    ids,
    forms,
    controls: { channel, display, offset, scale, relative, autoRange, horizontalOffset, level, dmmFunction, currentType, range, dmmHold, waveform, frequencyHz, load, output, symmetryPercent },
    toastMessages,
    dmmRead,
    autoAction,
    dmmHold,
  };
}

class FakeEventSource {
  static instances = [];

  constructor(url) {
    this.url = url;
    this.closed = false;
    this.readyState = FakeEventSource.CONNECTING;
    this.listeners = new Map();
    FakeEventSource.instances.push(this);
  }

  addEventListener(type, listener) {
    const callbacks = this.listeners.get(type) || [];
    callbacks.push(listener);
    this.listeners.set(type, callbacks);
  }

  emit(type, value) {
    const data = typeof value === "string" ? value : JSON.stringify(value);
    for (const listener of this.listeners.get(type) || []) listener({ data });
  }

  open() {
    this.readyState = FakeEventSource.OPEN;
    this.onopen();
  }

  fail(readyState) {
    this.readyState = readyState;
    this.onerror();
  }

  close() {
    this.closed = true;
    this.readyState = FakeEventSource.CLOSED;
  }
}

FakeEventSource.CONNECTING = 0;
FakeEventSource.OPEN = 1;
FakeEventSource.CLOSED = 2;

async function startBrowser({ hydrationFails = false, hydrationResponse, respond, dmmResponse, hidden = false } = {}) {
  const document = createDocument();
  document.hidden = hidden;
  const window = createElement();
  const fetchCalls = [];
  const fetch = async (url, options = {}) => {
    fetchCalls.push({ url, options });
    if (url === "/api/dmm/measurement") return dmmResponse ? dmmResponse(url, options) : { ok: true, text: async () => JSON.stringify({ raw: "0", capturedAt: "2026-09-19T12:00:00Z" }) };
    if (hydrationFails) throw new Error(`HTTP failed for ${url}`);
    if (hydrationResponse && (url === "/api/device" || url === "/api/state")) return hydrationResponse(url);
    if (respond && url !== "/api/device" && url !== "/api/state") return respond(url, options);
    const body = url === "/api/device" ? { manufacturer: "OWON", model: "test" } : { acquisition: { mode: "ACQUISITION_MODE_SAMPLE" } };
    return { ok: true, status: 200, text: async () => JSON.stringify(body) };
  };
  const timers = new Map();
  const blobs = [];
  let now = 0;
  let timerID = 0;
  const schedule = (callback, delay) => { timers.set(++timerID, { callback, delay }); return timerID; };
  const runTimers = (delay) => {
    for (const [id, timer] of [...timers]) {
      if (timer.delay !== delay) continue;
      timers.delete(id);
      timer.callback();
    }
  };
  const context = {
    AbortController,
    Blob,
    URL: { createObjectURL(blob) { blobs.push(blob); return "blob:fixture"; }, revokeObjectURL() {} },
    EventSource: FakeEventSource,
    Promise,
    TextDecoder,
    atob,
    clearTimeout: id => timers.delete(id),
    console,
    document,
    fetch,
    setTimeout: schedule,
    URLSearchParams,
    window,
    performance: { now: () => now },
  };
  vm.runInNewContext(appSource, context, { filename: "app.js" });
  await new Promise((resolve) => setImmediate(resolve));
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(FakeEventSource.instances.length, 1);
  return { context, document, window, source: FakeEventSource.instances[0], fetchCalls, runTimers, timers, blobs, advance(ms) { now += ms; runTimers(500); } };
}

async function main() {
  const added = { generator: testGeneratorValidation, generatorBounds: testGeneratorBounds, generatorRevisions: testGeneratorDrafts, generatorResult: testGeneratorResultMessages, trace: testScreenTrace, cursor: testScreenCursors, cachedRaw: testCachedRawByteCount, traceLifecycle: testTraceLifecycle, traceHydration: testTraceHydration, manual: testManualCapture, local: testLocalFeedback, auto: testAutoAction, observed: testObservedReadouts, provenance: testDMMProvenance, timestamps: testDMMCaptureTimestamps, independent: testIndependentDMM, ownership: testDMMOwnership, hold: testDMMHostHold };
  if (process.env.UI_TEST_CASE) return added[process.env.UI_TEST_CASE]();
  for (const test of Object.values(added)) await test();
  await testLiveDMM();
  await testStreamAttributeNormalization();
  await testPageRestoration();
  await testHydrationOwnership();
  await testDependentForms();
  await testDependentRevisions();
  FakeEventSource.instances = [];
  const first = await startBrowser();
  assert.equal(first.document.ids.get("device-meta").textContent, "Identity loaded · no additional metadata");
  assert.equal(first.document.ids.get("metric-acquisition").textContent, "Sample");
  first.source.open();
  first.source.emit("state", { state: { measurements: [{ channel: "CHANNEL_1", kind: "MEASUREMENT_KIND_PEAK_TO_PEAK", unit: "V" }] } });
  assert.equal(first.document.ids.get("metric-ch1").textContent, "0", "omitted protobuf scalar is zero");
  first.source.emit("state", { state: { measurements: [{ channel: "CHANNEL_1", kind: "MEASUREMENT_KIND_PEAK_TO_PEAK", value: 5, unit: "V" }] } });
  assert.equal(first.document.ids.get("metric-ch1").textContent, "5");
  first.source.emit("state", { state: {} });
  assert.equal(first.document.ids.get("metric-ch1").textContent, "—", "absent measurement is unavailable");
  assert.equal(first.document.ids.get("metric-acquisition").textContent, "—");
  for (const [range, label] of [["DMM_RANGE_MV", "mV"], ["DMM_RANGE_V", "V"], ["DMM_RANGE_ON", "ON"]]) {
    first.source.emit("state", { state: { dmm: { range } } });
    assert.equal(first.document.ids.get("dmm-range-state").textContent, label);
  }
  first.source.emit("state", { state: { dmm: { range: "DMM_RANGE_UNSPECIFIED", observedRangeToken: "10A" } } });
  assert.equal(first.document.ids.get("dmm-range-state").textContent, "Unknown (raw: 10A)");
  first.source.emit("state", { state: { dmm: { range: "DMM_RANGE_UNSPECIFIED" } } });
  assert.equal(first.document.ids.get("dmm-range-state").textContent, "Range unavailable");
  first.source.emit("state", { state: {} });
  assert.equal(first.document.ids.get("dmm-range-state").textContent, "Range unavailable");
  first.source.emit("state", { sequence: "10", state: {} });
  first.source.emit("state", { sequence: "12", state: {} });
  first.source.emit("waveform-gap", { sequence: "13", waveformGap: { missingCount: "2" } });
  assert.equal(first.document.ids.get("event-gaps").textContent, "1");
  assert.equal(first.document.ids.get("waveform-gaps").textContent, "2");
  first.source.emit("service-error", { sequence: "14", error: { message: "temporary", retryable: true } });
  assert.equal(first.source.closed, false);
  first.source.emit("service-error", { sequence: "15", error: { message: "unsupported", retryable: false } });
  assert.equal(first.source.closed, true);

  first.document.forms[0].dispatch("submit", { preventDefault() { this.defaultPrevented = true; } });
  await new Promise((resolve) => setImmediate(resolve));
  const untouchedChannelRequest = first.fetchCalls.find((call) => call.url === "/api/channel");
  assert.deepEqual(JSON.parse(untouchedChannelRequest.options.body), { channel: "CHANNEL_1" });
  first.document.dispatch("change", { target: first.document.controls.display });
  first.document.dispatch("change", { target: first.document.controls.offset });
  first.document.forms[0].dispatch("submit", { preventDefault() { this.defaultPrevented = true; } });
  await new Promise((resolve) => setImmediate(resolve));
  const channelRequest = first.fetchCalls.filter((call) => call.url === "/api/channel")[1];
  assert.ok(channelRequest);
  assert.deepEqual(JSON.parse(channelRequest.options.body), { channel: "CHANNEL_1", display: false, offsetDivisions: "0" });
  first.document.dispatch("change", { target: first.document.controls.scale });
  first.document.forms[0].dispatch("submit", { preventDefault() {} });
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(JSON.parse(first.fetchCalls.filter((call) => call.url === "/api/channel")[2].options.body), { channel: "CHANNEL_1", scale: "500mV" });
  first.document.dmmRead.dispatch("click");
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(first.document.ids.get("metric-dmm").textContent, "0 (unit unknown)");
  first.document.forms[1].dispatch("submit", { preventDefault() { this.defaultPrevented = true; } });
  await new Promise((resolve) => setImmediate(resolve));
  const dmmRequest = first.fetchCalls.find((call) => call.url === "/api/dmm");
  assert.deepEqual(JSON.parse(dmmRequest.options.body), { autoRange: true });

  first.document.ids.get("stream-interval").value = "250";
  first.document.ids.get("stream-queue").value = "64";
  first.document.ids.get("stream-header").checked = true;
  first.document.ids.get("stream-wave-ch1").checked = true;
  first.document.ids.get("stream-apply").dispatch("click");
  assert.equal(FakeEventSource.instances.length, 2);
  const second = FakeEventSource.instances[1];
  assert.match(second.url, /interval=250ms/);
  assert.match(second.url, /queue_capacity=64/);
  assert.match(second.url, /include_screen_header=true/);
  assert.match(second.url, /waveform_channel=CHANNEL_1/);
  second.open();
  assert.equal(first.document.ids.get("event-sequence").textContent, "—");
  assert.equal(first.document.ids.get("event-gaps").textContent, "0");
  assert.equal(first.document.ids.get("waveform-gaps").textContent, "0");
  const staleHydration = first.document.ids.get("hydration-status").textContent;
  first.source.open();
  first.source.emit("state", { sequence: "99", state: { device: { model: "stale" } } });
  first.source.emit("waveform", { sequence: "100", waveform: { channel: "CHANNEL_1", data: "AQ==" } });
  first.source.emit("waveform-gap", { sequence: "101", waveformGap: { missingCount: "9" } });
  first.source.emit("service-error", { sequence: "102", error: { message: "stale", retryable: false } });
  first.source.emit("stream-error", { code: "Internal", message: "stale", retryable: false });
  first.source.fail(FakeEventSource.CONNECTING);
  first.source.fail(FakeEventSource.CLOSED);
  assert.equal(second.closed, false);
  assert.equal(first.document.ids.get("event-sequence").textContent, "—");
  assert.equal(first.document.ids.get("waveform-gaps").textContent, "0");
  assert.equal(first.document.ids.get("hydration-status").textContent, staleHydration);
  assert.match(first.document.ids.get("hydration-status").textContent, /awaiting fresh state/);
  second.emit("state", { sequence: "12", state: {} });
  assert.equal(first.document.ids.get("event-gaps").textContent, "0");
  assert.equal(first.document.ids.get("hydration-status").className, "status-line success");

  second.fail(FakeEventSource.CONNECTING);
  assert.equal(second.closed, false);
  assert.equal(first.document.ids.get("connection-label").textContent, "Reconnecting to device…");
  assert.equal(first.document.ids.get("event-retries").textContent, "1");
  const reconnectToasts = first.document.toastMessages.length;
  second.fail(FakeEventSource.CONNECTING);
  assert.equal(second.closed, false);
  assert.equal(first.document.toastMessages.length, reconnectToasts);
  assert.equal(first.document.ids.get("event-retries").textContent, "1");
  second.open();

  second.emit("stream-error", { code: "Unavailable", message: "temporary", retryable: true });
  assert.equal(second.closed, false);
  assert.equal(first.document.ids.get("connection-label").textContent, "Reconnecting to device…");
  assert.equal(first.document.ids.get("event-retries").textContent, "2");
  const streamErrorToasts = first.document.toastMessages.length;
  second.emit("stream-error", { code: "Unavailable", message: "temporary", retryable: true });
  assert.equal(first.document.ids.get("event-retries").textContent, "2");
  second.fail(FakeEventSource.CONNECTING);
  assert.equal(second.closed, false);
  assert.equal(first.document.toastMessages.length, streamErrorToasts);
  second.emit("stream-error", { code: "InvalidArgument", message: "terminal", retryable: false });
  assert.equal(second.closed, true);
  assert.equal(first.document.ids.get("connection-label").textContent, "Stream stopped");
  assert.match(first.document.ids.get("hydration-status").textContent, /stale/);

  first.document.ids.get("stream-apply").dispatch("click");
  const closed = FakeEventSource.instances[2];
  closed.open();
  assert.match(first.document.ids.get("hydration-status").textContent, /awaiting fresh state/);
  assert.equal(first.document.ids.get("event-retries").textContent, "0");
  closed.fail(FakeEventSource.CLOSED);
  assert.equal(closed.closed, true);
  assert.equal(first.document.ids.get("connection-label").textContent, "Stream stopped");

  FakeEventSource.instances = [];
  const recovered = await startBrowser({ hydrationFails: true });
  recovered.source.open();
  assert.match(recovered.document.ids.get("hydration-status").textContent, /awaiting fresh state/);
  recovered.source.emit("state", { sequence: "1", state: {} });
  assert.equal(recovered.document.ids.get("hydration-status").className, "status-line success");
  assert.equal(recovered.document.ids.get("connection-label").textContent, "Live device stream");

  const retryMatrix = [
    { name: "true", event: { message: "temporary", retryable: true }, retry: true },
    { name: "false", event: { message: "terminal", retryable: false }, retry: false },
    { name: "absent", event: { message: "missing" }, retry: false },
    { name: "null", event: { message: "null", retryable: null }, retry: false },
    { name: "string", event: { message: "string", retryable: "true" }, retry: false },
    { name: "object", event: { message: "object", retryable: {} }, retry: false },
    { name: "malformed", event: "{not-json", retry: false },
  ];
  for (const test of retryMatrix) {
    FakeEventSource.instances = [];
    const browser = await startBrowser();
    browser.source.open();
    browser.source.emit("stream-error", test.event);
    assert.equal(browser.source.closed, !test.retry, test.name);
    assert.equal(browser.document.ids.get("event-retries").textContent, test.retry ? "1" : "0", test.name);
  }
  for (const test of retryMatrix) {
    FakeEventSource.instances = [];
    const browser = await startBrowser();
    browser.source.open();
    if (typeof test.event === "string") {
      browser.source.emit("service-error", test.event);
    } else {
      browser.source.emit("service-error", { sequence: "2", error: test.event });
    }
    assert.equal(browser.source.closed, !test.retry, `service ${test.name}`);
    assert.equal(browser.document.ids.get("event-retries").textContent, test.retry ? "1" : "0", `service ${test.name}`);
  }

  FakeEventSource.instances = [];
  const nativeReopen = await startBrowser();
  nativeReopen.source.open();
  nativeReopen.source.emit("state", { sequence: "10", state: {} });
  nativeReopen.source.emit("state", { sequence: "12", state: {} });
  nativeReopen.source.emit("waveform-gap", { sequence: "13", waveformGap: { missingCount: "2" } });
  assert.equal(nativeReopen.document.ids.get("event-sequence").textContent, "13");
  assert.equal(nativeReopen.document.ids.get("event-gaps").textContent, "1");
  assert.equal(nativeReopen.document.ids.get("waveform-gaps").textContent, "2");
  nativeReopen.source.fail(FakeEventSource.CONNECTING);
  assert.equal(nativeReopen.document.ids.get("event-sequence").textContent, "13");
  assert.equal(nativeReopen.document.ids.get("event-gaps").textContent, "1");
  assert.equal(nativeReopen.document.ids.get("waveform-gaps").textContent, "2");
  nativeReopen.source.open();
  assert.equal(nativeReopen.document.ids.get("event-sequence").textContent, "—");
  assert.equal(nativeReopen.document.ids.get("event-gaps").textContent, "0");
  assert.equal(nativeReopen.document.ids.get("waveform-gaps").textContent, "0");
  assert.equal(nativeReopen.document.ids.get("event-retries").textContent, "1");
  nativeReopen.source.emit("state", { sequence: "1", state: {} });
  assert.equal(nativeReopen.document.ids.get("event-sequence").textContent, "1");
  assert.equal(nativeReopen.document.ids.get("event-gaps").textContent, "0");

  nativeReopen.document.ids.get("stream-apply").dispatch("click");
  const replacement = FakeEventSource.instances[1];
  replacement.open();
  replacement.emit("state", { sequence: "10", state: {} });
  replacement.emit("state", { sequence: "12", state: {} });
  replacement.emit("waveform-gap", { sequence: "13", waveformGap: { missingCount: "2" } });
  nativeReopen.source.open();
  assert.equal(nativeReopen.document.ids.get("event-sequence").textContent, "13");
  assert.equal(nativeReopen.document.ids.get("event-gaps").textContent, "1");
  assert.equal(nativeReopen.document.ids.get("waveform-gaps").textContent, "2");

  for (const value of [undefined, 0, 12.5, -3]) {
    FakeEventSource.instances = [];
    const browser = await startBrowser({ dmmResponse: async () => ({ ok: true, text: async () => JSON.stringify({ ...observation(), value, unit: "V" }) }) });
    browser.document.dmmRead.dispatch("click");
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(browser.document.ids.get("metric-dmm").textContent, `${value ?? 0} V`);
  }
  FakeEventSource.instances = [];
  let acknowledge;
  const pending = await startBrowser({ respond: () => new Promise((resolve) => { acknowledge = resolve; }) });
  const edited = pending.document.controls.scale;
  pending.document.dispatch("input", { target: edited });
  pending.document.forms[0].dispatch("submit", { preventDefault() {} });
  edited.value = "1V";
  pending.document.dispatch("input", { target: edited });
  acknowledge({ ok: true, text: async () => "{}" });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(edited.dataset.touched, "true", "in-flight edits remain dirty");
  pending.document.forms[0].dispatch("submit", { preventDefault() {} });
  assert.equal(JSON.parse(pending.fetchCalls.at(-1).options.body).scale, "1V");
  acknowledge({ ok: true, text: async () => "{}" });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(edited.dataset.touched, undefined, "acknowledged revision is cleared");
  pending.source.emit("stream-error", { retryable: false, message: "stopped" });
  assert.equal(pending.document.forms[0].elements.at(-1).disabled, false, "stopped SSE does not disable unary controls");
  FakeEventSource.instances = [];
  const offsets = await startBrowser();
  offsets.source.open();
  offsets.source.emit("state", { sequence: "1", state: { channels: [{ screenHeaderOffset: -78 }], horizontal: { screenHeaderOffset: 1.25 } } });
  assert.equal(offsets.document.controls.offset.value, "0", "raw header does not populate channel input");
  assert.equal(offsets.document.controls.horizontalOffset.value, "", "raw header does not populate horizontal input");
  assert.equal(offsets.document.controls.offset.dataset.touched, undefined);
  assert.match(offsets.document.ids.get("state-json").textContent, /screenHeaderOffset/);
  for (const [control, form, values] of [
    [offsets.document.controls.offset, offsets.document.forms[0], ["-200", "0", "200"]],
    [offsets.document.controls.horizontalOffset, offsets.document.forms[2], ["-9223372036854775808", "9007199254740993", "9223372036854775807", "0"]],
  ]) {
    for (const value of values) {
      control.value = value;
      offsets.document.dispatch("input", { target: control });
      form.dispatch("submit", { preventDefault() {} });
      await new Promise((resolve) => setImmediate(resolve));
      assert.equal(JSON.parse(offsets.fetchCalls.at(-1).options.body).offsetDivisions, value);
      assert.equal(control.dataset.touched, undefined);
    }
    for (const value of ["0.25", "1e2", "NaN", "Infinity", "9223372036854775808", "-9223372036854775809", ...(control.min ? ["-201", "201"] : [])]) {
      const before = offsets.fetchCalls.length;
      control.value = value;
      offsets.document.dispatch("input", { target: control });
      form.dispatch("submit", { preventDefault() {} });
      await new Promise((resolve) => setImmediate(resolve));
      assert.equal(offsets.fetchCalls.length, before, `invalid offset ${value} must not send HTTP`);
      assert.equal(control.dataset.touched, "true");
      assert.match(offsets.document.ids.get("toast").textContent, /whole divisions|range/);
    }
  }
  offsets.document.dispatch("input", { target: offsets.document.controls.level });
  offsets.document.forms[3].dispatch("submit", { preventDefault() {} });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(JSON.parse(offsets.fetchCalls.at(-1).options.body).levelVolts, 0.25, "trigger remains fractional");
  offsets.document.controls.level.name = "offsetVolts";
  offsets.document.forms[3].dataset.api = "/api/generator";
  offsets.document.dispatch("input", { target: offsets.document.controls.level });
  offsets.document.forms[3].dispatch("submit", { preventDefault() {} });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(JSON.parse(offsets.fetchCalls.at(-1).options.body).offsetVolts, 0.25, "generator remains fractional");
  console.log("ui state transitions, zero readings and revision acknowledgement: ok");
}

async function testStreamAttributeNormalization() {
  FakeEventSource.instances = [];
  const { document } = await startBrowser();
  const interval = document.ids.get("stream-interval");
  const queue = document.ids.get("stream-queue");
  Object.assign(interval, { min: "30", max: "500", defaultValue: "250", value: "" });
  Object.assign(queue, { min: "2", max: "32", defaultValue: "16", value: "0" });
  document.ids.get("stream-apply").dispatch("click");
  assert.match(FakeEventSource.instances.at(-1).url, /interval=250ms&queue_capacity=16/);
  interval.value = "1"; queue.value = "9999";
  document.ids.get("stream-apply").dispatch("click");
  assert.match(FakeEventSource.instances.at(-1).url, /interval=30ms&queue_capacity=32/);
  assert.equal(Number(interval.value), 30); assert.equal(Number(queue.value), 32);
  interval.value = "900"; queue.value = "-5";
  document.ids.get("stream-apply").dispatch("click");
  assert.match(FakeEventSource.instances.at(-1).url, /interval=500ms&queue_capacity=2/);
}

async function testPageRestoration() {
  FakeEventSource.instances = [];
  const browser = await startBrowser();
  const { document, window, source, fetchCalls } = browser;
  const label = () => document.ids.get("connection-label").textContent;
  const hydration = () => document.ids.get("hydration-status").textContent;
  const snapshot = { state: { device: { model: "fresh stream" } } };
  source.open(); source.emit("state", snapshot);
  assert.equal(label(), "Live device stream");
  window.dispatch("beforeunload");
  assert.equal(source.closed, false, "aborted navigation does not close the stream");
  document.controls.scale.value = "1.00V";
  document.dispatch("input", { target: document.controls.scale });
  const revision = document.controls.scale.dataset.revision;
  const appliedURL = source.url;
  document.ids.get("stream-interval").value = "2345";
  let current = source;
  for (let cycle = 0; cycle < 2; cycle++) {
    window.dispatch("pagehide", { persisted: true });
    assert.equal(current.closed, true);
    assert.notEqual(label(), "Live device stream");
    assert.match(hydration(), /stale|suspend/i);
    current.emit("state", { state: { device: { model: "obsolete" } } });
    current.open();
    assert.notEqual(label(), "Live device stream");
    assert.doesNotMatch(document.ids.get("device-model").textContent, /obsolete/);
    const count = FakeEventSource.instances.length;
    window.dispatch("pageshow", { persisted: false });
    assert.equal(FakeEventSource.instances.length, count);
    window.dispatch("pageshow", { persisted: true });
    assert.equal(FakeEventSource.instances.length, count + 1);
    current = FakeEventSource.instances.at(-1);
    assert.equal(current.url, appliedURL, "restore does not apply stream drafts");
    assert.notEqual(label(), "Live device stream");
    window.dispatch("pageshow", { persisted: true });
    assert.equal(FakeEventSource.instances.length, count + 1, "duplicate restore has one owner");
    current.open();
    assert.notEqual(label(), "Live device stream");
    current.emit("state", snapshot);
    assert.equal(label(), "Live device stream");
    assert.equal(document.controls.scale.value, "1.00V");
    assert.equal(document.controls.scale.dataset.revision, revision);
    assert.equal(document.controls.scale.dataset.touched, "true");
    assert.equal(document.ids.get("stream-interval").value, "2345");
    assert.equal(fetchCalls.filter(call => call.url !== "/api/dmm/measurement").length, 2, "restore does not rehydrate or submit drafts");
  }
  current.emit("stream-error", { retryable: false, message: "terminal" });
  const count = FakeEventSource.instances.length;
  window.dispatch("pagehide", { persisted: true });
  window.dispatch("pageshow", { persisted: true });
  window.dispatch("pageshow", { persisted: true });
  assert.equal(FakeEventSource.instances.length, count, "terminal survives restoration");
  assert.notEqual(label(), "Live device stream");
  document.ids.get("stream-apply").dispatch("click");
  assert.equal(FakeEventSource.instances.length, count + 1, "explicit Apply may restart terminal");
  assert.match(FakeEventSource.instances.at(-1).url, /interval=2345ms/);
  assert.notEqual(label(), "Live device stream");
}

async function testHydrationOwnership() {
  for (const replacement of ["apply", "suspend", "restore"]) {
    FakeEventSource.instances = [];
    const held = new Map();
    const browser = await startBrowser({ hydrationResponse: url => new Promise(resolve => held.set(url, resolve)) });
    if (replacement === "apply") {
      browser.document.ids.get("stream-interval").value = "400";
      browser.document.ids.get("stream-apply").dispatch("click");
      const current = FakeEventSource.instances.at(-1);
      current.open(); current.emit("state", { state: { device: { model: "new owner" } } });
    } else {
      browser.window.dispatch("pagehide", { persisted: true });
      if (replacement === "restore") browser.window.dispatch("pageshow", { persisted: true });
    }
    for (const resolve of held.values()) resolve({ ok: true, text: async () => JSON.stringify({ model: "obsolete hydration", device: { model: "obsolete hydration" } }) });
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(FakeEventSource.instances.length, replacement === "suspend" ? 1 : 2);
    assert.doesNotMatch(browser.document.ids.get("device-model").textContent, /obsolete hydration/);
    if (replacement === "apply") {
      assert.equal(browser.document.ids.get("device-model").textContent, "OWON new owner");
      assert.equal(browser.document.ids.get("connection-label").textContent, "Live device stream");
    } else {
      browser.window.dispatch("pageshow", { persisted: true });
      assert.equal(FakeEventSource.instances.length, 2);
      assert.notEqual(browser.document.ids.get("connection-label").textContent, "Live device stream");
    }
    assert.equal(browser.fetchCalls.filter(call => call.url !== "/api/dmm/measurement").length, 2);
  }
}

async function testDependentForms() {
  FakeEventSource.instances = [];
  const browser = await startBrowser();
  const { document, fetchCalls } = browser;
  const controls = document.controls;
  const change = (name, value) => {
    controls[name].value = value;
    document.dispatch("input", { target: controls[name] });
  };
  const submit = async (index) => {
    document.forms[index].dispatch("submit", { preventDefault() {} });
    await new Promise(resolve => setImmediate(resolve));
  };
  const bodies = (path) => fetchCalls.filter(call => call.url === path).map(call => JSON.parse(call.options.body));
  change("waveform", "GENERATOR_WAVEFORM_SINE");
  change("frequencyHz", "100");
  change("load", "GENERATOR_LOAD_ON");
  await submit(4);
  change("frequencyHz", "200");
  await submit(4);
  assert.deepEqual(bodies("/api/generator")[1], { waveform: "GENERATOR_WAVEFORM_SINE", frequencyHz: 200 }, "frequency keeps its selected companion without replaying load");
  change("waveform", "GENERATOR_WAVEFORM_SQUARE");
  await submit(4);
  assert.deepEqual(bodies("/api/generator")[2], { waveform: "GENERATOR_WAVEFORM_SQUARE" }, "waveform-only does not replay frequency");
  change("waveform", "");
  change("frequencyHz", "300");
  await submit(4);
  assert.equal(bodies("/api/generator").length, 3, "missing waveform sends no HTTP request");
  assert.match(document.toastMessages.at(-1), /select.*waveform/i);
  change("dmmFunction", "DMM_FUNCTION_VOLTAGE");
  await submit(1);
  assert.equal(bodies("/api/dmm").length, 0, "missing AC/DC is local");
  change("currentType", "DMM_CURRENT_TYPE_DC");
  change("range", "DMM_RANGE_V");
  await submit(1);
  change("currentType", "DMM_CURRENT_TYPE_AC");
  await submit(1);
  assert.deepEqual(bodies("/api/dmm")[1], { function: "DMM_FUNCTION_VOLTAGE", currentType: "DMM_CURRENT_TYPE_AC", autoRange: true });
  change("dmmFunction", "DMM_FUNCTION_CURRENT");
  await submit(1);
  assert.deepEqual(bodies("/api/dmm")[2], { function: "DMM_FUNCTION_CURRENT", currentType: "DMM_CURRENT_TYPE_AC", autoRange: true });
  change("dmmFunction", "DMM_FUNCTION_RESISTANCE");
  assert.equal(controls.currentType.value, "");
  assert.equal(controls.currentType.disabled, true);
  await submit(1);
  assert.deepEqual(bodies("/api/dmm")[3], { function: "DMM_FUNCTION_RESISTANCE", autoRange: true });
  change("dmmFunction", "DMM_FUNCTION_VOLTAGE");
  await submit(1);
  assert.equal(bodies("/api/dmm").length, 4, "reset AC/DC requires a new selection");
  change("currentType", "DMM_CURRENT_TYPE_DC");
  change("dmmFunction", "DMM_FUNCTION_DIODE");
  await submit(1);
  assert.equal(bodies("/api/dmm").length, 4, "dirty incompatible AC/DC cannot disappear through disabled omission");
  assert.equal(controls.currentType.dataset.touched, "true");
  assert.equal(controls.currentType.disabled, false);
  assert.match(document.toastMessages.at(-1), /clear.*current type/i);
  change("currentType", "");
  await submit(1);
  assert.deepEqual(bodies("/api/dmm")[4], { function: "DMM_FUNCTION_DIODE", autoRange: true });
  change("dmmFunction", "");
  change("currentType", "DMM_CURRENT_TYPE_AC");
  await submit(1);
  assert.equal(bodies("/api/dmm").length, 5, "AC/DC needs an explicit compatible function");
  change("symmetryPercent", "50.5");
  await submit(4);
  assert.equal(bodies("/api/generator").length, 3, "fractional symmetry remains local");
  assert.match(document.toastMessages.at(-1), /whole.*percent/i);
}

async function testDependentRevisions() {
  for (const [formIndex, fields, initial, newer] of [
    [4, ["waveform", "frequencyHz"], ["GENERATOR_WAVEFORM_SINE", "100"], ["GENERATOR_WAVEFORM_SQUARE", "200"]],
    [1, ["dmmFunction", "currentType"], ["DMM_FUNCTION_VOLTAGE", "DMM_CURRENT_TYPE_DC"], ["DMM_FUNCTION_CURRENT", "DMM_CURRENT_TYPE_AC"]],
  ]) {
    for (const changed of [[0], [1], [0, 1]]) {
      FakeEventSource.instances = [];
      let acknowledge;
      const browser = await startBrowser({ respond: () => new Promise(resolve => { acknowledge = resolve; }) });
      const form = browser.document.forms[formIndex];
      const controls = fields.map(name => browser.document.controls[name]);
      const edit = (index, value) => {
        controls[index].value = value;
        browser.document.dispatch("input", { target: controls[index] });
      };
      initial.forEach((value, index) => edit(index, value));
      form.dispatch("submit", { preventDefault() {} });
      changed.forEach(index => edit(index, newer[index]));
      acknowledge({ ok: true, text: async () => "{}" });
      await new Promise(resolve => setImmediate(resolve));
      controls.forEach((control, index) => assert.equal(control.dataset.touched, changed.includes(index) ? "true" : undefined));
      form.dispatch("submit", { preventDefault() {} });
      const body = JSON.parse(browser.fetchCalls.at(-1).options.body);
      for (const index of changed) assert.equal(String(body[controls[index].name]), newer[index]);
      if (formIndex === 1 || changed.includes(1)) {
        controls.forEach((control, index) => assert.equal(String(body[control.name]), changed.includes(index) ? newer[index] : initial[index]));
      }
      acknowledge({ ok: false, status: 400, text: async () => JSON.stringify({ message: "rejected patch" }) });
      await new Promise(resolve => setImmediate(resolve));
      changed.forEach(index => assert.equal(controls[index].dataset.touched, "true", "failed requests preserve every dirty edit"));
      assert.match(browser.document.toastMessages.at(-1), /rejected patch/);
      form.dispatch("submit", { preventDefault() {} });
      acknowledge({ ok: true, text: async () => "{}" });
      await new Promise(resolve => setImmediate(resolve));
      controls.forEach(control => assert.equal(control.dataset.touched, undefined));
    }
  }
}

const settle = () => new Promise(resolve => setImmediate(resolve));
const observation = (value = 0) => ({ value, raw: String(value), capturedAt: "2026-09-19T12:00:00Z" });

async function testLocalFeedback() {
  FakeEventSource.instances = [];
  let acknowledge;
  const browser = await startBrowser({ respond: () => new Promise(resolve => { acknowledge = resolve; }) });
  const { document, source, fetchCalls } = browser;
  const form = document.forms[1];
  const fieldError = document.ids.get("dmm-current-error");
  const fieldGroup = document.ids.get("dmm-current-field");
  const edit = (name, value) => {
    document.controls[name].value = value;
    document.dispatch("input", { target: document.controls[name] });
  };
  edit("dmmFunction", "DMM_FUNCTION_CURRENT");
  form.dispatch("submit", { preventDefault() {} });
  assert.match(form.feedback.textContent, /Select AC or DC/);
  assert.match(fieldError.textContent, /Select AC or DC/);
  assert.equal(document.controls.currentType.getAttribute("aria-invalid"), "true");
  assert.match(document.controls.currentType.getAttribute("aria-describedby"), /dmm-current-error/);
  assert.equal(document.controls.currentType.focused, true);
  assert.equal(document.controls.currentType.focusOptions.preventScroll, true);
  assert.equal(fieldGroup.scrollOptions.block, "center");
  assert.equal(fieldGroup.scrollOptions.behavior, "instant");
  assert.equal(fetchCalls.filter(call => call.url === "/api/dmm").length, 0);
  source.emit("state", { state: {} });
  source.emit("waveform-gap", { waveformGap: { missingCount: "1" } });
  browser.runTimers(7000);
  assert.match(form.feedback.textContent, /Select AC or DC/, "stream notifications never erase local feedback");
  assert.match(fieldError.textContent, /Select AC or DC/, "field error survives live updates and elapsed toast timers");
  const focusCount = document.controls.currentType.focusCount;
  const scrollCount = fieldGroup.scrollCount;
  edit("currentType", "DMM_CURRENT_TYPE_DC");
  assert.notEqual(document.controls.currentType.getAttribute("aria-invalid"), "true");
  assert.equal(fieldError.textContent, "");
  assert.equal(document.controls.currentType.focusCount, focusCount, "correction never steals focus");
  assert.equal(fieldGroup.scrollCount, scrollCount, "correction never scrolls the page");
  form.dispatch("submit", { preventDefault() {} });
  assert.match(form.feedback.textContent, /Sending/);
  assert.equal(form.getAttribute("aria-busy"), "true");
  assert.equal(form.querySelector("button[type=submit]").disabled, true);
  assert.equal(form.querySelector("button[type=submit]").getAttribute("aria-busy"), "true");
  const post = fetchCalls.find(call => call.url === "/api/dmm");
  assert.deepEqual(JSON.parse(post.options.body), { function: "DMM_FUNCTION_CURRENT", currentType: "DMM_CURRENT_TYPE_DC", autoRange: true });
  edit("currentType", "DMM_CURRENT_TYPE_AC");
  acknowledge({ ok: true, text: async () => "{}" });
  await settle();
  assert.match(form.feedback.textContent, /acknowledged/i);
  assert.equal(document.controls.currentType.dataset.touched, "true");
  assert.equal(form.getAttribute("aria-busy"), "false");
  assert.equal(form.querySelector("button[type=submit]").getAttribute("aria-busy"), "false");
  form.dispatch("submit", { preventDefault() {} });
  acknowledge({ ok: false, status: 503, text: async () => JSON.stringify({ message: "Device unavailable" }) });
  await settle();
  assert.match(form.feedback.textContent, /Device unavailable/);
  edit("dmmFunction", "DMM_FUNCTION_DIODE");
  form.dispatch("submit", { preventDefault() {} });
  assert.match(form.feedback.textContent, /Clear Current type/);
  assert.match(fieldError.textContent, /Clear Current type/);
  assert.equal(document.controls.currentType.getAttribute("aria-invalid"), "true");
  const conflictFocusCount = document.controls.currentType.focusCount;
  edit("currentType", "DMM_CURRENT_TYPE_DC");
  assert.match(fieldError.textContent, /Clear Current type/, "edits retain an unresolved active error");
  assert.equal(document.controls.currentType.focusCount, conflictFocusCount);
  edit("currentType", "");
  assert.notEqual(document.controls.currentType.getAttribute("aria-invalid"), "true");
  assert.equal(fieldError.textContent, "");
}

async function testAutoAction() {
  FakeEventSource.instances = [];
  const browser = await startBrowser();
  browser.document.autoAction.dispatch("click");
  await settle();
  const request = browser.fetchCalls.find(call => call.url === "/api/auto");
  assert.ok(request);
  assert.equal(request.options.method, "POST");
  assert.equal(request.options.body, "");
  assert.equal(browser.document.toastMessages.at(-1), "Auto command sent; device response/readback unavailable; effect unverified.");
  assert.doesNotMatch(browser.document.toastMessages.at(-1), /applied/i);
}

async function testObservedReadouts() {
  FakeEventSource.instances = [];
  const { document, source } = await startBrowser();
  const output = id => document.ids.get(id).textContent;
  document.controls.scale.value = "draft";
  document.dispatch("input", { target: document.controls.scale });
  const revision = document.controls.scale.dataset.revision;
  const kinds = ["MAXIMUM", "MINIMUM", "PEAK_TO_PEAK", "AMPLITUDE", "AVERAGE", "PERIOD", "FREQUENCY"];
  source.emit("state", { state: {
    channels: [{ channel: "CHANNEL_1", coupling: "COUPLING_DC", scale: "500mV", screenHeaderOffset: 0 }, { channel: "CHANNEL_2", display: true, probeAttenuation: 10, coupling: "COUPLING_AC", scale: "2V", screenHeaderOffset: -12 }],
    horizontal: { scale: "1ms", screenHeaderOffset: 0 }, acquisition: { mode: "ACQUISITION_MODE_SAMPLE", memoryDepth: "4K" },
    trigger: { source: "TRIGGER_SOURCE_CHANNEL_1", slope: "TRIGGER_SLOPE_RISING", coupling: "COUPLING_DC", sweep: "TRIGGER_SWEEP_AUTO", status: "WAIT" },
    measurements: kinds.map(kind => ({ channel: "CHANNEL_1", kind: `MEASUREMENT_KIND_${kind}`, unit: kind === "PERIOD" ? "s" : kind === "FREQUENCY" ? "Hz" : "V" })),
  } });
  assert.match(output("observed-ch1"), /Display: Off.*Coupling: DC.*Probe: unavailable.*Scale: 500mV.*raw.*unknown.*0/i);
  assert.match(output("observed-ch2"), /Display: On.*Coupling: AC.*Probe: 10.*Scale: 2V.*-12/);
  assert.match(output("observed-horizontal"), /Scale: 1ms.*raw.*unknown.*0/i);
  assert.match(output("observed-acquisition"), /Mode: Sample.*Depth: 4K/);
  assert.match(output("observed-trigger"), /Source: Channel 1.*Slope: Rising.*Coupling: DC.*Level: 0 V.*Sweep: Auto.*Status: WAIT/);
  const rows = document.ids.get("measurements").children;
  assert.equal(rows.length, 7);
  kinds.forEach((kind, index) => assert.match(rows[index].textContent, new RegExp(`Channel 1.*${kind.replaceAll("_", " ")}.*0`, "i")));
  assert.equal(document.controls.scale.value, "draft");
  assert.equal(document.controls.scale.dataset.revision, revision);
  assert.equal(document.controls.scale.dataset.touched, "true");
  assert.equal(document.controls.offset.value, "0");
  assert.equal(document.controls.offset.dataset.touched, undefined);
  source.emit("state", { state: { channels: [{ channel: "CHANNEL_1" }], horizontal: {}, trigger: {} } });
  assert.match(output("observed-ch1"), /Scale: unavailable.*raw.*unavailable/i);
  assert.match(output("observed-ch1"), /Probe: unavailable/);
  source.emit("state", { state: { channels: [{ channel: "CHANNEL_1", probeAttenuation: 0 }] } });
  assert.match(output("observed-ch1"), /Probe: unavailable/, "zero attenuation is not a valid observed probe factor");
  assert.match(output("observed-ch2"), /unavailable/);
  assert.match(output("observed-acquisition"), /unavailable/);
  assert.match(output("observed-horizontal"), /unavailable/);
  assert.equal(document.ids.get("measurements").children.length, 1);
  assert.match(document.ids.get("measurements").children[0].textContent, /No measurements/);
}

async function testDMMProvenance() {
  FakeEventSource.instances = [];
  let body = { raw: "0", capturedAt: "2026-09-19T12:00:00Z" };
  const { document, source, runTimers } = await startBrowser({ dmmResponse: async () => ({ ok: true, text: async () => JSON.stringify(body) }) });
  source.emit("state", { state: {} });
  await settle();
  assert.equal(document.ids.get("metric-dmm").textContent, "0 (unit unknown)");
  const capture = document.ids.get("dmm-status").textContent.split("last captured ")[1];
  for (const bad of [{}, [], null, { value: 2 }, { ...observation(), raw: "" }, { ...observation(), raw: 0 }, { ...observation(), capturedAt: "" }, { ...observation(), capturedAt: "nonsense" }, { ...observation(), capturedAt: 42 }, ...[null, "3", "NaN", "Infinity", 1e400].map(value => ({ ...observation(), value }))]) {
    body = bad;
    runTimers(1000);
    await settle();
    assert.match(document.ids.get("dmm-status").textContent, /Stale.*DMM.*(protocol|measurement)/i, JSON.stringify(bad));
    assert.equal(document.ids.get("metric-dmm").textContent, "0 (unit unknown)");
    assert.equal(document.ids.get("dmm-status").textContent.split("last captured ")[1], capture);
  }
  body = { ...observation(2), unit: "A" };
  runTimers(1000); await settle();
  assert.equal(document.ids.get("metric-dmm").textContent, "2 A");
  assert.match(document.ids.get("dmm-status").textContent, /Automatic updates/);
  assert.doesNotMatch(document.ids.get("dmm-status").textContent, /every second/);
  for (const unit of [{}, 5, [], "", null, undefined]) {
    body = { ...observation(3), unit };
    if (unit === undefined) delete body.unit;
    runTimers(1000); await settle();
    assert.equal(document.ids.get("metric-dmm").textContent, "3 (unit unknown)", JSON.stringify(unit));
    assert.match(document.ids.get("dmm-status").textContent, /Automatic updates/, JSON.stringify(unit));
  }
}

async function testDMMCaptureTimestamps() {
  FakeEventSource.instances = [];
  let body = { ...observation(7), unit: "A", capturedAt: "2024-02-29T15:30:20.123456789+05:30" };
  const { document, runTimers } = await startBrowser({ dmmResponse: async () => ({ ok: true, text: async () => JSON.stringify(body) }) });
  const status = document.ids.get("dmm-status");
  const reading = document.ids.get("metric-dmm");
  const captureLabel = status.textContent.split("last captured ")[1];
  const originalCapture = body.capturedAt;
  const invalid = [
    "1", "2024-02-30T00:00:00Z", "2025-02-29T00:00:00Z", "1900-02-29T00:00:00Z", "2100-02-29T00:00:00Z", "2024-04-31T00:00:00Z",
    "0000-01-01T00:00:00Z", "10000-01-01T00:00:00Z", "2024-00-01T00:00:00Z", "2024-13-01T00:00:00Z", "2024-01-00T00:00:00Z", "2024-01-32T00:00:00Z",
    "2024-01-01T24:00:00Z", "2024-01-01T00:60:00Z", "2024-01-01T00:00:60Z", "2024-01-01T00:00:00+24:00", "2024-01-01T00:00:00-24:00", "2024-01-01T00:00:00+01:60",
    "2024-01-01T00:00:00+1:00", "2024-01-01T00:00:00+0100", "2024-01-01T00:00:00+01", "2024-01-01T00:00:00.1234567890Z", "2024-01-01T00:00:00.Z",
    "2024-1-01T00:00:00Z", "2024-01-1T00:00:00Z", "2024-01-01T0:00:00Z", "2024-01-01T00:0:00Z", "2024-01-01T00:00:0Z",
    "2024-01-01", "2024-01-01T00:00:00", "1/1/2024", "2024-01-01t00:00:00z", " 2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z ", "2024-01-01T00:00:00Z\n",
    "0001-01-01T00:00:00+00:01", "9999-12-31T23:59:59.999999999-00:01",
  ];
  for (const capturedAt of invalid) {
    body = { ...observation(999), unit: "V", capturedAt };
    runTimers(1000); await settle();
    assert.match(status.textContent, /Stale.*DMM protocol error/, capturedAt);
    assert.equal(reading.textContent, "7 A", capturedAt);
    assert.equal(status.textContent.split("last captured ")[1], captureLabel, capturedAt);
    assert.equal(status.title, originalCapture, "full timestamp provenance survives rejection");
  }
  const valid = [
    ["2000-02-29T00:00:00Z", "2000-02-29T00:00:00.000Z"],
    ["2024-02-29T00:00:00Z", "2024-02-29T00:00:00.000Z"],
    ["0001-01-01T00:00:00Z", "0001-01-01T00:00:00.000Z"],
    ["9999-12-31T23:59:59.999999999Z", "9999-12-31T23:59:59.999Z"],
    ["0001-01-01T05:30:00+05:30", "0001-01-01T00:00:00.000Z"],
    ["9999-12-31T15:59:59.999999999-08:00", "9999-12-31T23:59:59.999Z"],
    ["2024-02-29T00:00:00-00:00", "2024-02-29T00:00:00.000Z"],
    ["2024-03-01T00:00:00+23:59", "2024-02-29T00:01:00.000Z"],
    ["2024-02-29T00:00:00-23:59", "2024-02-29T23:59:00.000Z"],
    ...Array.from({ length: 9 }, (_, index) => {
      const fraction = "123456789".slice(0, index + 1);
      return [`2024-02-29T10:20:30.${fraction}Z`, `2024-02-29T10:20:30.${fraction.padEnd(3, "0").slice(0, 3)}Z`];
    }),
  ];
  for (const [index, [capturedAt, utc]] of valid.entries()) {
    body = { ...observation(index), unit: "A", capturedAt };
    runTimers(1000); await settle();
    assert.match(status.textContent, /^Automatic updates/, capturedAt);
    assert.equal(reading.textContent, `${index} A`, capturedAt);
    assert.equal(status.textContent.split("last captured ")[1], new Date(utc).toLocaleString(), capturedAt);
    assert.equal(status.title, capturedAt, "valid timestamp retains offset and full fractional precision");
  }
}

async function testIndependentDMM() {
  for (const delayed of [true, false]) {
    FakeEventSource.instances = [];
    const pending = [];
    const browser = await startBrowser({ hydrationResponse: delayed ? () => new Promise(() => {}) : undefined, hydrationFails: !delayed, dmmResponse: (url, options) => new Promise(resolve => pending.push({ resolve, options })) });
    assert.equal(pending.length, 1, "DMM starts despite delayed or failed scope hydration");
    if (browser.source) browser.source.emit("stream-error", { retryable: false, message: "terminal" });
    pending.shift().resolve({ ok: true, text: async () => JSON.stringify(observation()) });
    await settle();
    browser.runTimers(1000);
    assert.equal(pending.length, 1, "terminal scope stream cannot stop DMM");
    browser.window.dispatch("pagehide", { persisted: true });
  }
}

async function testDMMOwnership() {
  FakeEventSource.instances = [];
  const pending = [];
  const browser = await startBrowser({ hidden: true, dmmResponse: (url, options) => new Promise((resolve, reject) => pending.push({ resolve, reject, options })) });
  const { document, window, runTimers, timers, fetchCalls } = browser;
  const count = () => fetchCalls.filter(call => call.url === "/api/dmm/measurement").length;
  const status = () => document.ids.get("dmm-status").textContent;
  const polls = () => [...timers.values()].filter(timer => timer.delay === 1000).length;
  const respond = async (request, value) => {
    request.resolve({ ok: true, text: async () => JSON.stringify(observation(value)) });
    await settle();
  };
  assert.equal(count(), 0, "initially hidden page does not query DMM");
  document.hidden = false;
  document.dispatch("visibilitychange");
  document.dispatch("visibilitychange");
  assert.equal(count(), 1);
  assert.equal(polls(), 0, "no polling timer until request completion");
  runTimers(1000);
  assert.equal(count(), 1, "slow read never overlaps");
  const old = pending.shift();
  window.dispatch("pagehide", { persisted: true });
  assert.equal(old.options.signal.aborted, true);
  window.dispatch("pageshow", { persisted: true });
  window.dispatch("pageshow", { persisted: true });
  document.dispatch("visibilitychange");
  document.dmmRead.dispatch("click");
  assert.equal(count(), 1, "restore waits for canceled request to settle");
  await respond(old, 999);
  assert.equal(document.ids.get("metric-dmm").textContent, "", "obsolete completion is not displayed");
  assert.equal(count(), 2, "one current read follows cancellation");
  assert.equal(document.dmmRead.disabled, true, "replacement request owns the busy control");
  assert.equal(pending.length, 1, "stale completion does not overlap or schedule an extra request");
  await respond(pending.shift(), 2);
  assert.equal(polls(), 1, "exactly one completion-scheduled poll");
  document.dmmRead.dispatch("click");
  document.dmmRead.dispatch("click");
  assert.equal(polls(), 0, "manual refresh cancels scheduled poll");
  assert.equal(count(), 3);
  const timeoutRequest = pending.shift();
  timeoutRequest.options.signal.addEventListener("abort", () => timeoutRequest.reject(Object.assign(new Error("aborted"), { name: "AbortError" })));
  runTimers(15000);
  await settle();
  assert.match(status(), /Stale.*timed out.*retrying/);
  assert.equal(document.ids.get("metric-dmm").textContent, "2 (unit unknown)");
  assert.equal(polls(), 1);
  runTimers(1000);
  const abandoned = pending.shift();
  document.hidden = true;
  document.dispatch("visibilitychange");
  await respond(abandoned, 998);
  assert.equal(polls(), 0, "obsolete hidden completion never restarts polling");
  runTimers(1000);
  assert.equal(count(), 4);
  assert.equal(document.ids.get("metric-dmm").textContent, "2 (unit unknown)");
}

async function testDMMHostHold() {
  FakeEventSource.instances = [];
  const pending = [];
  const browser = await startBrowser({ dmmResponse: () => new Promise((resolve, reject) => pending.push({ resolve, reject })) });
  const { document, runTimers, timers, fetchCalls } = browser;
  const count = () => fetchCalls.filter(call => call.url === "/api/dmm/measurement").length;
  const polls = () => [...timers.values()].filter(timer => timer.delay === 1000).length;
  const reading = () => document.ids.get("metric-dmm").textContent;
  const status = () => document.ids.get("dmm-status").textContent;
  const answer = async (request, value, capturedAt) => {
    request.resolve({ ok: true, text: async () => JSON.stringify({ ...observation(value), capturedAt }) });
    await settle();
  };

  assert.equal(count(), 1);
  await answer(pending.shift(), 1, "2026-09-19T12:00:00Z");
  assert.equal(reading(), "1 (unit unknown)");
  const captured = status().split("last captured ")[1];
  document.dmmHold.dispatch("click");
  assert.equal(document.dmmHold.getAttribute("aria-pressed"), "true");
  assert.equal(document.dmmHold.textContent, "Resume live display");
  assert.match(status(), /Host hold/);
  assert.equal(status().split("last captured ")[1], captured);

  runTimers(1000);
  assert.equal(count(), 2);
  runTimers(1000);
  assert.equal(count(), 2, "host hold keeps one in-flight owner");
  await answer(pending.shift(), 2, "2026-09-19T13:00:00Z");
  assert.equal(reading(), "1 (unit unknown)", "host hold freezes the displayed fresh value");
  assert.match(status(), /Host hold/);
  assert.equal(status().split("last captured ")[1], captured, "host hold freezes the displayed timestamp");
  assert.equal(polls(), 1, "held refresh still schedules one normal retry");

  document.dmmHold.dispatch("click");
  assert.equal(document.dmmHold.getAttribute("aria-pressed"), "false");
  assert.equal(document.dmmHold.textContent, "Hold display");
  assert.equal(reading(), "2 (unit unknown)", "resuming displays the newest fresh reading");
  assert.match(status(), /Automatic updates/);
  assert.equal(count(), 2, "resuming does not issue an extra request");
  assert.equal(polls(), 1, "resuming preserves the existing retry owner");
}

async function testLiveDMM() {
  FakeEventSource.instances = [];
  const pending = [];
  const respond = (url, options) => new Promise(resolve => pending.push({ resolve, options }));
  const browser = await startBrowser({ respond, dmmResponse: respond });
  const { document, source, window, runTimers, fetchCalls } = browser;
  const settle = () => new Promise(resolve => setImmediate(resolve));
  const answer = async (body, ok = true) => {
    pending.shift().resolve({ ok, status: ok ? 200 : 503, text: async () => JSON.stringify(Object.hasOwn(body, "value") ? { ...observation(body.value), ...body } : body) });
    await settle();
  };
  const count = () => fetchCalls.filter(call => call.url === "/api/dmm/measurement").length;
  const status = () => document.ids.get("dmm-status").textContent;
  const reading = () => document.ids.get("metric-dmm").textContent;
  const fresh = (current = source) => current.emit("state", { state: {} });
  source.open();
  assert.equal(count(), 1, "page activation starts DMM independently of scope state");
  fresh();
  assert.equal(count(), 1, "fresh stream state leaves the independent DMM owner running");
  fresh();
  document.dmmRead.dispatch("click");
  runTimers(1000);
  assert.equal(count(), 1, "automatic, stream, and manual triggers share one in-flight read");
  await answer({ value: 0, unit: "A", capturedAt: "2026-09-19T12:00:00Z" });
  assert.equal(reading(), "0 A");
  assert.match(status(), /Automatic updates.*last captured/);
  assert.equal(document.toastMessages.length, 0, "live readings do not replace command feedback");
  runTimers(1000);
  assert.doesNotMatch(status(), /Stale/, "healthy automatic refresh does not invalidate prior data");
  await answer({ value: 2 });
  assert.equal(reading(), "2 (unit unknown)", "missing metadata does not borrow a requested unit");
  runTimers(1000);
  await answer({ message: "device unavailable" }, false);
  assert.match(status(), /Stale.*device unavailable.*retrying/);
  assert.equal(reading(), "2 (unit unknown)", "failed refresh retains but marks the last reading stale");
  runTimers(1000);
  assert.match(status(), /Stale/, "failed reading remains stale while its retry runs");
  const hiddenRead = pending[0];
  document.hidden = true;
  document.dispatch("visibilitychange");
  assert.equal(hiddenRead.options.signal.aborted, true);
  await answer({ value: 999, unit: "V" });
  runTimers(1000);
  assert.equal(reading(), "2 (unit unknown)", "hidden-page completion cannot overwrite the reading");
  assert.match(status(), /Stale.*hidden/);
  const beforeResume = count();
  document.hidden = false;
  document.dispatch("visibilitychange");
  document.dispatch("visibilitychange");
  assert.equal(count(), beforeResume + 1, "visible page starts one read");
  source.fail(FakeEventSource.CONNECTING);
  await answer({ value: 998, unit: "V" });
  assert.equal(reading(), "998 V");
  runTimers(1000);
  assert.equal(count(), beforeResume + 2);
  source.open();
  fresh();
  await answer({ value: -0.5, unit: "A" });
  assert.equal(reading(), "-0.5 A");
  runTimers(1000);
  window.dispatch("pagehide", { persisted: true });
  await answer({ value: 997, unit: "V" });
  assert.match(status(), /suspended/);
  assert.equal(reading(), "-0.5 A");
  window.dispatch("pageshow", { persisted: true });
  const restored = FakeEventSource.instances.at(-1);
  restored.open();
  fresh(restored);
  await answer({ value: 0.25, unit: "A" });
  runTimers(1000);
  restored.emit("stream-error", { retryable: false, message: "terminal" });
  await answer({ value: 996, unit: "V" });
  const stoppedCount = count();
  runTimers(1000);
  document.dispatch("visibilitychange");
  assert.equal(count(), stoppedCount + 1, "terminal scope stream does not stop automatic DMM reads");
  assert.equal(reading(), "996 V");
  document.ids.get("stream-apply").dispatch("click");
  const restarted = FakeEventSource.instances.at(-1);
  restarted.open();
  fresh(restarted);
  await answer({ value: 1, unit: "A" });
  assert.equal(reading(), "1 A", "stream Apply leaves the independent DMM read running");
  runTimers(1000);
  const oldRead = pending.shift();
  const controls = document.controls;
  controls.dmmFunction.value = "DMM_FUNCTION_CURRENT";
  controls.currentType.value = "DMM_CURRENT_TYPE_DC";
  document.dispatch("input", { target: controls.dmmFunction });
  document.forms[1].dispatch("submit", { preventDefault() {} });
  assert.equal(oldRead.options.signal.aborted, true, "applying settings invalidates the earlier read");
  fresh(restarted);
  assert.equal(pending.length, 1, "settings application never starts another read");
  oldRead.resolve({ ok: true, text: async () => JSON.stringify({ value: 995, unit: "V" }) });
  await settle();
  await answer({});
  assert.equal(reading(), "1 A");
  assert.equal(pending.length, 1, "acknowledgement starts an observed read");
  await answer({ value: 0.75, unit: "A" });
  assert.equal(reading(), "0.75 A");
  console.log("live DMM: serialized updates, unknown metadata, errors, hide, restore, reconnect, terminal and settings ownership: ok");
}

const traceCapture = (channel = "CHANNEL_1", sequence = "1", overrides = {}) => ({
  channel, sequence, data: "AAH/", screenHeaderJson: "eyJyYXciOjF9", captureStartedAt: "2026-09-20T12:00:01Z", capturedAt: "2026-09-20T12:00:00Z",
  screenTrace: { profile: "fixture", width: 4, height: 20, horizontalDivisions: 2, verticalDivisions: 4, y: [-2, 0, 20, 25] }, ...overrides,
});
const descendants = element => [element, ...element.children.flatMap(descendants)];
const tracePaths = document => descendants(document.ids.get("scope-plots")).filter(e => e.getAttribute("class")?.includes("scope-trace"));

async function testScreenTrace() {
  FakeEventSource.instances = [];
  const { document, source, blobs } = await startBrowser();
  source.open();
  source.emit("waveform", { sequence: "1", waveform: traceCapture() });
  let plots = document.ids.get("scope-plots").children;
  assert.equal(plots.length, 1);
  assert.equal(plots[0].getAttribute("viewBox"), "0 0 4 20");
  assert.equal(tracePaths(document)[0].getAttribute("d"), "M0 -2 L1 0 L2 20 L3 25");
  assert.ok(descendants(plots[0]).some(e => e.tagName === "clipPath"));
  assert.match(document.ids.get("scope-ch1-status").textContent, /ground.*0/i);
  source.emit("waveform", { sequence: "2", waveform: traceCapture("CHANNEL_2") });
  assert.equal(document.ids.get("scope-plots").children.length, 1, "matching geometry shares a grid");
  assert.equal(tracePaths(document).length, 2);
  assert.equal(tracePaths(document)[1].getAttribute("stroke-dasharray"), "6 3");
  document.ids.get("raw-source").value = "CHANNEL_1";
  document.ids.get("raw-source").dispatch("change");
  document.ids.get("inspect-header").dispatch("click");
  const frozen = document.ids.get("waveform-header").textContent;
  document.ids.get("download-waveform").dispatch("click");
  assert.deepEqual([...new Uint8Array(await blobs.at(-1).arrayBuffer())], [0, 1, 255]);
  assert.equal(document.ids.get("download-trace-csv").disabled, false);
  assert.equal(document.ids.get("download-trace-svg").disabled, false);
  document.ids.get("download-trace-csv").dispatch("click");
  assert.match(await blobs.at(-1).text(), /x_screen,y_screen,ground_y/);
  assert.match(await blobs.at(-1).text(), /0,-2,0/);
  document.ids.get("download-trace-svg").dispatch("click");
  assert.match(await blobs.at(-1).text(), /^<\?xml/);
  assert.match(await blobs.at(-1).text(), /viewBox="0 0 4 20"/);
  source.emit("waveform", { sequence: "3", waveform: traceCapture("CHANNEL_2", "2", { screenTrace: { profile: "different", width: 2, height: 12, horizontalDivisions: 1, verticalDivisions: 3, y: [1, 4], groundY: -1 } }) });
  assert.equal(document.ids.get("scope-plots").children.length, 2, "different geometry remains separate");
  assert.match(document.ids.get("scope-ch2-status").textContent, /ground.*off.screen/i);
  assert.equal(document.ids.get("raw-source").value, "CHANNEL_1");
  assert.equal(document.ids.get("waveform-header").textContent, frozen);
  source.emit("waveform", { sequence: "4", waveform: traceCapture("CHANNEL_1", "2", { screenTrace: undefined, screenTraceUnavailableReason: "PEAK unsupported" }) });
  assert.equal(tracePaths(document).length, 1);
  assert.equal(document.ids.get("scope-cursor-panels").children.length, 1, "valid CH2 trace retains only its own cursor panel");
  assert.match(document.ids.get("scope-ch1-status").textContent, /PEAK unsupported/);
  assert.equal(document.ids.get("download-waveform").disabled, false);
  assert.equal(document.ids.get("download-trace-csv").disabled, true, "invalid trace does not export as calibrated data");
  assert.equal(document.ids.get("download-trace-svg").disabled, true, "invalid trace does not export as a plot");
  let sequence = 3;
  for (const changes of [
    { screenTrace: { ...traceCapture().screenTrace, y: [1] } },
    { screenTrace: { ...traceCapture().screenTrace, groundY: null } },
    { screenTrace: { ...traceCapture().screenTrace, y: [0, 1, 2, 2147483648] } },
    { screenTrace: { ...traceCapture().screenTrace, width: 16385, y: Array(16385).fill(0) } },
    { screenTrace: { ...traceCapture().screenTrace, verticalDivisions: 129 } },
    { screenTraceUnavailableReason: "both" },
    { captureStartedAt: "2026-02-30T00:00:00Z" },
  ]) {
    source.emit("waveform", { waveform: traceCapture("CHANNEL_1", String(sequence++), changes) });
    assert.equal(tracePaths(document).length, 1);
    assert.equal(document.ids.get("scope-cursor-panels").children.length, 1, "malformed CH1 trace gets no cursor overlay of its own");
    assert.match(document.ids.get("scope-ch1-status").textContent, /Render error|browser plot limit/i);
    assert.equal(document.ids.get("download-waveform").disabled, false, "bad trace preserves independently valid bytes");
  }
  source.emit("waveform", { waveform: traceCapture("CHANNEL_1", String(sequence), { data: "A".repeat(12 * 1024 * 1024), screenHeaderJson: "A".repeat(12 * 1024 * 1024) }) });
  assert.equal(document.ids.get("download-waveform").disabled, true);
  assert.equal(document.ids.get("inspect-header").disabled, true);
  assert.match(document.ids.get("waveform-result").textContent, /limit/);
}

async function testScreenCursors() {
  FakeEventSource.instances = [];
  const browser = await startBrowser();
  const { document, source, fetchCalls } = browser;
  assert.equal(document.ids.get("scope-cursor-panels").children.length, 0, "no capture has no cursor panel");
  source.open();
  source.emit("waveform", { sequence: "1", waveform: traceCapture() });
  assert.equal(document.ids.get("scope-cursor-panels").children.length, 1);
  const panel = document.ids.get("scope-cursor-panels").children[0];
  const panelText = () => descendants(document.ids.get("scope-cursor-panels").children[0]).map(child => child.textContent).join(" ");
  const buttons = descendants(panel).filter(child => child.tagName === "button");
  assert.equal(buttons.length, 2, "cursor A and B controls are visible");
  assert.equal(buttons[0].textContent, "Cursor A");
  assert.equal(buttons[1].textContent, "Cursor B");
  assert.equal(document.ids.get("scope-plots").children[0].getAttribute("tabindex"), "0");
  assert.equal(buttons[0].getAttribute("aria-label"), "Select cursor A");
  assert.equal(buttons[1].getAttribute("aria-label"), "Select cursor B");
  assert.match(panelText(), /ΔB-A=\(x unknown screen slots, y unknown screen counts; x right-positive, y down-positive\)/);
  assert.doesNotMatch(panelText(), /(?:\bmV\b|\bV\b|\bms\b|\bs\b|ΔV|ΔT|trigger|calibrat)/i, "cursor readout has no electrical/time calibration units");
  buttons[0].focus();
  assert.equal(buttons[0].focused, true);
  const svg = document.ids.get("scope-plots").children[0];
  svg.getBoundingClientRect = () => ({ left: 100, top: 50, width: 400, height: 200 });
  const requests = fetchCalls.length;
  svg.dispatch("pointerdown", { clientX: 300, clientY: 150, pointerType: "mouse", preventDefault() { this.prevented = true; } });
  assert.match(panelText(), /A=\(x 2 screen slots, y 10 screen counts\)/);
  assert.match(panelText(), /B=\(unset\)/);
  assert.equal(fetchCalls.length, requests, "pointer cursor interaction is browser-local");
  buttons[1].dispatch("click");
  assert.equal(buttons[1].getAttribute("aria-pressed"), "true");
  assert.equal(buttons[0].getAttribute("aria-pressed"), "false");
  svg.dispatch("pointerdown", { clientX: 500, clientY: 250, pointerType: "touch", preventDefault() { this.prevented = true; } });
  assert.match(panelText(), /B=\(x 3 screen slots, y 20 screen counts\)/);
  assert.match(panelText(), /ΔB-A=\(x \+1 screen slots, y \+10 screen counts; x right-positive, y down-positive\)/);
  browser.context.window.devicePixelRatio = 2;
  svg.getBoundingClientRect = () => ({ left: 200, top: 100, width: 800, height: 400 });
  svg.dispatch("touchstart", { touches: [{ clientX: 200, clientY: 100 }], preventDefault() {} });
  assert.match(panelText(), /B=\(x 0 screen slots, y 0 screen counts\)/, "touch uses CSS coordinates at DPR 2");
  svg.dispatch("pointerdown", { clientX: 1000, clientY: 500, pointerType: "mouse", preventDefault() {} });
  assert.match(panelText(), /B=\(x 3 screen slots, y 20 screen counts\)/, "pointer edges clamp to host bounds");
  buttons[0].dispatch("click");
  let prevented = false;
  svg.dispatch("keydown", { key: "ArrowLeft", shiftKey: false, preventDefault() { prevented = true; } });
  assert.equal(prevented, true);
  assert.match(panelText(), /A=\(x 1 screen slots, y 10 screen counts\)/);
  svg.dispatch("keydown", { key: "ArrowDown", shiftKey: true, preventDefault() {} });
  assert.match(panelText(), /A=\(x 1 screen slots, y 15 screen counts\)/);
  for (let index = 0; index < 8; index += 1) svg.dispatch("keydown", { key: "ArrowUp", shiftKey: true, preventDefault() {} });
  assert.match(panelText(), /A=\(x 1 screen slots, y 0 screen counts\)/, "keyboard movement clamps without wrapping");
  source.emit("waveform", { sequence: "2", waveform: traceCapture("CHANNEL_1", "2") });
  assert.match(descendants(document.ids.get("scope-cursor-panels")).map(child => child.textContent).join(" "), /A=\(x 1 screen slots, y 0 screen counts\)/, "same geometry retains cursor points");
  source.emit("waveform", { sequence: "2b", waveform: traceCapture("CHANNEL_2", "2") });
  assert.equal(document.ids.get("scope-cursor-panels").children.length, 1, "same geometry channels share one cursor pair");
  browser.advance(3100);
  assert.match(panelText(), /Stale/i, "stale retained traces keep the cursor overlay but say stale");
  source.fail(FakeEventSource.CONNECTING);
  assert.match(panelText(), /Stale.*reconnecting/i);
  source.open();
  source.emit("waveform-gap", { sequence: "3", waveformGap: { channel: "CHANNEL_1", missingCount: "1" } });
  assert.match(panelText(), /Stale.*gap/i, "capture gaps visibly stale the cursor panel");
  source.emit("waveform", { sequence: "3", waveform: traceCapture("CHANNEL_2", "2", { screenTrace: { profile: "different", width: 2, height: 12, horizontalDivisions: 1, verticalDivisions: 3, y: [1, 4] } }) });
  assert.equal(document.ids.get("scope-cursor-panels").children.length, 2, "different geometry has an independent cursor panel");
  const secondText = descendants(document.ids.get("scope-cursor-panels").children[1]).map(child => child.textContent).join(" ");
  assert.equal(secondText.includes("A=(unset)"), true);
  assert.match(secondText, /screen geometry changed/i);
  source.emit("waveform", { sequence: "4", waveform: traceCapture("CHANNEL_2", "3", { screenTrace: undefined, screenTraceUnavailableReason: "unsupported" }) });
  assert.equal(document.ids.get("scope-cursor-panels").children.length, 1, "unsupported trace has no cursor overlay panel");
  assert.match(document.ids.get("scope-ch2-status").textContent, /unsupported/);
  source.emit("waveform", { sequence: "5", waveform: traceCapture("CHANNEL_1", "3", { screenTrace: undefined, screenTraceUnavailableReason: "unsupported" }) });
  assert.equal(document.ids.get("scope-cursor-panels").children.length, 0, "no renderable trace has no cursor overlay");
  source.emit("waveform", { sequence: "6", waveform: traceCapture("CHANNEL_1", "4", { screenTrace: { profile: "after-gap", width: 3, height: 9, horizontalDivisions: 3, verticalDivisions: 3, y: [1, 4, 8] } }) });
  assert.match(descendants(document.ids.get("scope-cursor-panels")).map(child => child.textContent).join(" "), /screen geometry changed/i, "a geometry change after an unsupported gap is announced");
}

async function testCachedRawByteCount() {
  FakeEventSource.instances = [];
  const { context, document, source, advance, blobs } = await startBrowser();
  source.open();
  let sequence = 0;
  for (const length of [0, 1, 2, 3, 8192]) {
    const bytes = Buffer.alloc(length, length === 1 ? 0 : 0xa5);
    source.emit("waveform", { waveform: traceCapture("CHANNEL_1", String(++sequence), { data: bytes.toString("base64") }) });
    assert.match(document.ids.get("waveform-result").textContent, new RegExp(`Raw bytes: ${length} ·`));
    assert.equal(document.ids.get("download-waveform").disabled, length === 0);
    assert.equal(document.ids.get("inspect-header").disabled, false, "empty raw bytes do not disable an independent header");
    if (length) {
      document.ids.get("download-waveform").dispatch("click");
      assert.deepEqual(Buffer.from(await blobs.at(-1).arrayBuffer()), bytes);
    }
  }
  const oversized = "A".repeat(12 * 1024 * 1024);
  for (const [data, header, bytes, rawDisabled, headerDisabled] of [
    [oversized, "e30=", 0, true, false],
    ["AA==", oversized, 1, false, true],
  ]) {
    source.emit("waveform", { waveform: traceCapture("CHANNEL_1", String(++sequence), { data, screenHeaderJson: header }) });
    assert.match(document.ids.get("waveform-result").textContent, new RegExp(`Raw bytes: ${bytes} ·`));
    assert.equal(document.ids.get("download-waveform").disabled, rawDisabled);
    assert.equal(document.ids.get("inspect-header").disabled, headerDisabled);
  }
  context.retainedRaw = Buffer.alloc(8192, 0xa5).toString("base64");
  vm.runInContext(`globalThis.rawScans = 0;
    const originalReplace = String.prototype.replace;
    String.prototype.replace = function (...args) {
      if (String(this) === retainedRaw) rawScans++;
      return originalReplace.apply(this, args);
    };`, context);
  source.emit("waveform", { waveform: traceCapture("CHANNEL_1", String(++sequence), { data: context.retainedRaw }) });
  assert.ok(context.rawScans > 0, "admission validates actual raw bytes before retaining them");
  context.rawScans = 0;
  for (let tick = 0; tick < 7; tick++) advance(500);
  assert.match(document.ids.get("waveform-result").textContent, /Stale.*Raw bytes: 8192 ·/s);
  assert.equal(context.rawScans, 0, "freshness ticks must not rescan retained raw bytes");
}

async function testTraceLifecycle() {
  FakeEventSource.instances = [];
  const { document, source, window, advance, timers, fetchCalls } = await startBrowser();
  source.open();
  assert.match(source.url, /waveform_channel=CHANNEL_1/); assert.match(source.url, /waveform_channel=CHANNEL_2/);
  const send = (channel, sequence, eventSequence) => source.emit("waveform", { sequence: eventSequence, waveform: traceCapture(channel, sequence) });
  const status = channel => document.ids.get(`scope-${channel}-status`).textContent;
  send("CHANNEL_1", "9007199254740993", "9007199254740993"); send("CHANNEL_2", "1", "9007199254740994");
  send("CHANNEL_1", "9007199254740992", "9007199254740995");
  assert.match(status("ch1"), /9007199254740993/);
  source.emit("waveform-gap", { sequence: "9007199254740996", waveformGap: { channel: "CHANNEL_1", missingCount: "3" } });
  assert.match(status("ch1"), /Stale.*3/); assert.doesNotMatch(status("ch2"), /Stale/);
  source.emit("state", { sequence: "9007199254740998", state: {} });
  assert.match(status("ch2"), /Stale.*sequence/i);
  send("CHANNEL_1", "9007199254740994", "9007199254740999");
  assert.doesNotMatch(status("ch1"), /Stale/); assert.match(status("ch2"), /Stale/);
  source.fail(FakeEventSource.CONNECTING); source.open();
  send("CHANNEL_1", "1", "1");
  assert.match(status("ch1"), /capture 1\b/); assert.match(status("ch2"), /Stale/);
  const count = fetchCalls.length;
  advance(3100); source.emit("state", { state: {} });
  assert.match(status("ch1"), /Stale.*received 3/); assert.equal(fetchCalls.length, count, "freshness clock performs no I/O");
  assert.match(document.ids.get("waveform-result").textContent, /Stale/, "selected raw capture also reports stale receipt age");
  assert.equal([...timers.values()].filter(t => t.delay === 500).length, 1);
  document.hidden = true; document.dispatch("visibilitychange");
  assert.equal([...timers.values()].filter(t => t.delay === 500).length, 0);
  document.hidden = false; document.dispatch("visibilitychange"); document.dispatch("visibilitychange");
  assert.equal([...timers.values()].filter(t => t.delay === 500).length, 1);
  document.ids.get("stream-wave-ch1").checked = false; document.ids.get("stream-apply").dispatch("click");
  assert.match(status("ch1"), /not subscribed/i);
  source.emit("waveform", { waveform: traceCapture("CHANNEL_1", "99") });
  assert.doesNotMatch(status("ch1"), /capture 99/);
  window.dispatch("pagehide", { persisted: true });
  assert.equal([...timers.values()].filter(t => t.delay === 500).length, 0);
}

async function testTraceHydration() {
  for (const outcome of ["state", "error", "reconnect"]) {
    FakeEventSource.instances = [];
    const pending = [];
    const { source, document } = await startBrowser({ hydrationResponse: () => new Promise(resolve => pending.push(resolve)) });
    source.open();
    source.emit("state", { state: { device: { model: "fresh stream" } } });
    if (outcome === "error") source.emit("stream-error", { retryable: false, message: "terminal" });
    if (outcome === "reconnect") source.fail(FakeEventSource.CONNECTING);
    const status = document.ids.get("hydration-status").textContent;
    pending.forEach(resolve => resolve({ ok: true, text: async () => JSON.stringify({ model: "obsolete", device: { model: "obsolete" } }) }));
    await settle();
    assert.match(document.ids.get("device-model").textContent, /fresh stream/);
    assert.equal(document.ids.get("hydration-status").textContent, status);
    assert.equal(FakeEventSource.instances.length, 1);
  }
}

async function testManualCapture() {
  FakeEventSource.instances = [];
  const pending = [];
  const { document, source, window } = await startBrowser({ respond: (url, options) => new Promise(resolve => pending.push({ resolve, options })) });
  source.open();
  const form = document.forms[5];
  form.dispatch("submit", { preventDefault() {} });
  source.emit("waveform", { waveform: traceCapture() });
  source.fail(FakeEventSource.CONNECTING); source.open();
  pending.shift().resolve({ ok: true, text: async () => JSON.stringify(traceCapture("CHANNEL_2", "0")) }); await settle();
  assert.equal(document.ids.get("raw-source").value, "manual", "reconnect does not invalidate a manual request");
  assert.match(document.ids.get("waveform-result").textContent, /Manual snapshot.*CHANNEL_2/s);
  assert.equal(tracePaths(document).length, 1, "manual response adds no third plot");
  const snapshot = document.ids.get("waveform-result").textContent;
  source.emit("waveform", { waveform: traceCapture("CHANNEL_1", "1") });
  assert.equal(document.ids.get("waveform-result").textContent, snapshot);
  form.dispatch("submit", { preventDefault() {} });
  pending.shift().resolve({ ok: false, status: 503, text: async () => '{"message":"capture failed"}' }); await settle();
  assert.match(document.ids.get("waveform-result").textContent, /Stale.*capture failed/s);
  form.dispatch("submit", { preventDefault() {} });
  const obsolete = pending.shift();
  window.dispatch("pagehide", { persisted: true });
  assert.equal(obsolete.options.signal.aborted, true);
  assert.match(form.feedback.textContent, /cancelled.*suspension/i);
  obsolete.resolve({ ok: true, text: async () => JSON.stringify(traceCapture()) }); await settle();
  assert.match(document.ids.get("waveform-result").textContent, /CHANNEL_2/);
}

const generatorBuiltins = ["AMP_ALT", "ATT_ALT", "STAIR_DOWN", "STAIR_UP_DOWN", "STAIR_UP", "BESSEL_J", "BESSEL_Y", "SINC"];
const generatorDependents = ["frequencyHz", "periodSeconds", "symmetryPercent", "dutyPercent", "pulseWidthSeconds", "risingSeconds", "fallingSeconds"];

async function generatorBrowser(options) {
  FakeEventSource.instances = [];
  const browser = await startBrowser(options);
  const form = browser.document.forms[4];
  return { ...browser, form,
    edit(name, value, badInput = false) {
      const input = form.elements[name];
      if (input.type === "checkbox") input.checked = value;
      else input.value = value;
      input.validity = { badInput };
      browser.document.dispatch("input", { target: input });
    },
    async submit() { form.dispatch("submit", { preventDefault() {} }); await settle(); },
    posts() { return browser.fetchCalls.filter(call => call.url === "/api/generator"); },
    error(name) { return browser.document.ids.get(`generator-${name}-error`).textContent; },
    marker(name) { return browser.document.ids.get(`generator-${name}-pending`).textContent; },
  };
}

async function testGeneratorValidation() {
  const empty = await generatorBrowser();
  await empty.submit();
  assert.equal(empty.posts().length, 0);
  assert.equal(empty.form.feedback.textContent, "No pending changes. No request sent.");
  for (const field of generatorDependents) {
    const b = await generatorBrowser();
    b.edit(field, "1"); await b.submit();
    assert.equal(b.posts().length, 0, field);
    assert.match(b.error("waveform"), /Select a waveform/);
    assert.match(b.form.feedback.textContent, /No request sent/);
    assert.equal(b.form.elements.waveform.focusOptions.preventScroll, true);
    const group = b.document.ids.get("generator-waveform-field");
    assert.equal(group.scrollOptions.block, "center");
    const persistentError = b.error("waveform");
    b.source.emit("state", { state: {} }); b.runTimers(7000);
    assert.equal(b.error("waveform"), persistentError, "stream updates and toast expiry preserve field feedback");
    const result = b.form.feedback.textContent, focuses = b.form.elements.waveform.focusCount;
    b.edit("waveform", "GENERATOR_WAVEFORM_SINE");
    assert.equal(b.error("waveform"), "");
    assert.equal(b.form.feedback.textContent, result, "editing cannot replace delivery feedback");
    assert.equal(b.form.elements.waveform.focusCount, focuses);
    await b.submit();
    assert.deepEqual(JSON.parse(b.posts()[0].options.body), { [field]: 1, waveform: "GENERATOR_WAVEFORM_SINE" });
  }
  const b = await generatorBrowser();
  b.edit("frequencyHz", "0"); await b.submit();
  assert.match(b.error("waveform"), /Select a waveform/, "invalid pending timing still requires its explicit companion");
  assert.ok(b.error("frequencyHz"));
  b.edit("waveform", "GENERATOR_WAVEFORM_SINE"); b.edit("frequencyHz", "1"); b.edit("periodSeconds", "1"); await b.submit();
  assert.equal(b.posts().length, 0); assert.match(b.error("periodSeconds"), /frequency.*period/i);
  const conflictResult = b.form.feedback.textContent;
  b.edit("frequencyHz", "");
  assert.equal(b.error("periodSeconds"), "", "clearing frequency revalidates period cross-field error");
  assert.equal(b.form.feedback.textContent, conflictResult);
  await b.submit(); assert.equal(b.posts().length, 1);
  for (const field of generatorDependents.slice(2)) {
    b.edit("waveform", "GENERATOR_WAVEFORM_SINC"); b.edit(field, "1"); await b.submit();
    assert.match(b.error(field), /Built-in/); assert.equal(b.posts().length, 1);
    const focusCount = b.form.elements[field].focusCount;
    b.edit("waveform", "GENERATOR_WAVEFORM_SINE");
    assert.equal(b.error(field), ""); assert.equal(b.form.elements[field].focusCount, focusCount);
    b.edit(field, "");
  }
  b.edit("waveform", ""); b.edit("frequencyHz", "", true); await b.submit();
  assert.match(b.error("frequencyHz"), /number/); assert.equal(b.posts().length, 1);
  assert.equal(b.marker("frequencyHz"), "Pending edit");
  b.edit("frequencyHz", "");
  assert.equal(b.error("frequencyHz"), ""); assert.equal(b.marker("frequencyHz"), "");
  await b.submit(); assert.equal(b.posts().length, 1);
  b.edit("output", false); b.edit("offsetVolts", "0"); await b.submit();
  assert.deepEqual(JSON.parse(b.posts()[1].options.body), { output: false, offsetVolts: 0 });
}

async function testGeneratorBounds() {
  for (const [suffix, maximum] of [["SINE", 25e6], ["SQUARE", 5e6], ["RAMP", 1e6], ["PULSE", 5e6], ...generatorBuiltins.map(name => [name, 5e6])]) {
    const b = await generatorBrowser();
    b.edit("waveform", `GENERATOR_WAVEFORM_${suffix}`);
    for (const [field, good, bad] of [["frequencyHz", [0.1, maximum], [0, 0.09, maximum + 1]], ["periodSeconds", [1 / maximum, 10], [0, 0.5 / maximum, 10.01]]]) {
      for (const value of good) {
        b.edit(field, String(value)); const count = b.posts().length; await b.submit();
        assert.equal(b.posts().length, count + 1, `${suffix} ${field} ${value}`);
        assert.deepEqual(JSON.parse(b.posts().at(-1).options.body), { waveform: `GENERATOR_WAVEFORM_${suffix}`, [field]: value });
      }
      for (const value of bad) {
        b.edit(field, String(value)); const count = b.posts().length; await b.submit();
        assert.equal(b.posts().length, count, `${suffix} ${field} ${value}`); assert.ok(b.error(field));
      }
      b.edit(field, "");
    }
  }
  for (const [field, values] of [["symmetryPercent", [-1, 100.1, 50.5]], ["dutyPercent", [-1, 100.1]], ["pulseWidthSeconds", [0, -1]], ["risingSeconds", [0, -1]], ["fallingSeconds", [0, -1]], ["amplitudeVolts", ["Infinity"]], ["offsetVolts", ["NaN"]]]) {
    const b = await generatorBrowser(); b.edit("waveform", "GENERATOR_WAVEFORM_SINE");
    for (const value of values) { b.edit(field, String(value)); await b.submit(); assert.equal(b.posts().length, 0); assert.ok(b.error(field)); }
  }
  const percentages = await generatorBrowser();
  percentages.edit("waveform", "GENERATOR_WAVEFORM_SINE");
  for (const value of [0, 100]) {
    percentages.edit("symmetryPercent", String(value)); percentages.edit("dutyPercent", String(value)); await percentages.submit();
    assert.deepEqual(JSON.parse(percentages.posts().at(-1).options.body), { waveform: "GENERATOR_WAVEFORM_SINE", symmetryPercent: value, dutyPercent: value });
  }
}

async function testGeneratorDrafts() {
  for (const outcome of ["success", "failure"]) {
    for (const newer of ["different", "away-back", "clear", "bad-input"]) {
      let resolve;
      const b = await generatorBrowser({ respond: () => new Promise(r => { resolve = r; }) });
      b.edit("waveform", "GENERATOR_WAVEFORM_SINE"); b.edit("frequencyHz", "100");
      await b.submit(); await b.submit(); assert.equal(b.posts().length, 1, "in-flight Apply cannot duplicate dispatch");
      const frozen = b.posts()[0].options.body;
      b.edit("frequencyHz", newer === "clear" || newer === "bad-input" ? "" : "200", newer === "bad-input");
      if (newer === "away-back") b.edit("frequencyHz", "100");
      assert.equal(b.posts()[0].options.body, frozen);
      resolve(outcome === "success" ? { ok: true, text: async () => "{}" } : { ok: false, status: 503, text: async () => '{"message":"write interrupted"}' });
      await settle();
      assert.match(b.form.feedback.textContent, outcome === "success" ? /Write request acknowledged; device readback unavailable/ : /delivery may be partial or unknown/);
      const pending = newer !== "clear";
      assert.equal(b.marker("frequencyHz"), pending ? "Pending edit" : "");
      assert.equal(b.form.elements.frequencyHz.dataset.touched, pending ? "true" : undefined);
      if (outcome === "success") {
        assert.equal(b.form.feedback.textContent.includes("Newer edits remain unsent"), pending);
        assert.equal(b.marker("waveform"), "Previously sent request");
      }
      if (newer === "bad-input") assert.match(b.error("frequencyHz"), /number/);
      const status = b.form.feedback.textContent;
      b.edit("frequencyHz", ""); assert.equal(b.form.feedback.textContent, status);
      b.runTimers(15000); b.runTimers(500); await settle(); assert.equal(b.posts().length, 1, "no automatic retries");
    }
  }
}

async function testGeneratorResultMessages() {
  const cases = [
    {
      name: "verified",
      body: {
        generator: {
          delivery: "GENERATOR_DELIVERY_TRANSPORT_COMPLETE_READBACK_VERIFIED",
          observation: { context: { match: "GENERATOR_CONTEXT_MATCH" }, output: { status: "GENERATOR_OBSERVATION_OBSERVED" } },
        },
      },
      expected: /logical waveform\/output readback verified \(not electrical validation\)/,
    },
    {
      name: "numeric-unverified",
      body: {
        generator: {
          delivery: "GENERATOR_DELIVERY_TRANSPORT_COMPLETE_READBACK_UNVERIFIED",
          observation: { context: { match: "GENERATOR_CONTEXT_MATCH" }, output: { status: "GENERATOR_OBSERVATION_OBSERVED" } },
        },
      },
      expected: /logical waveform\/output readback verified, numeric readback unverified/,
    },
    {
      name: "no-logical-readback",
      body: {
        generator: {
          delivery: "GENERATOR_DELIVERY_TRANSPORT_COMPLETE_READBACK_UNVERIFIED",
          observation: { context: { match: "GENERATOR_CONTEXT_UNSPECIFIED" }, output: { status: "GENERATOR_OBSERVATION_UNSPECIFIED" } },
        },
      },
      expected: /no logical generator readback was captured; numeric readback unverified/,
    },
    {
      name: "not-attempted",
      body: { generator: { delivery: "GENERATOR_DELIVERY_NOT_ATTEMPTED" } },
      expected: /No generator mutation was attempted; readback preflight did not converge/,
    },
    {
      name: "partial",
      body: { generator: { delivery: "GENERATOR_DELIVERY_PARTIAL_OR_UNKNOWN" } },
      expected: /Generator delivery is partial or unknown; review logical readback before retrying/,
    },
  ];
  for (const test of cases) {
    const browser = await generatorBrowser({ respond: () => ({ ok: true, text: async () => JSON.stringify(test.body) }) });
    browser.edit("waveform", "GENERATOR_WAVEFORM_SINE");
    await browser.submit();
    assert.match(browser.form.feedback.textContent, test.expected, test.name);
  }

  const failed = await generatorBrowser({
    respond: () => ({
      ok: false,
      status: 503,
      text: async () => JSON.stringify({
        code: "Unavailable",
        message: "set generator: restoration exchange lost",
        generator: {
          delivery: "GENERATOR_DELIVERY_PARTIAL_OR_UNKNOWN",
          observation: { context: { match: "GENERATOR_CONTEXT_MATCH" }, output: { status: "GENERATOR_OBSERVATION_UNKNOWN" } },
          compensation: { requested: false, status: "GENERATOR_OUTPUT_COMPENSATION_FAILED" },
        },
      }),
    }),
  });
  failed.edit("waveform", "GENERATOR_WAVEFORM_SINE");
  await failed.submit();
  assert.match(failed.form.feedback.textContent, /Request failed; Generator delivery is partial or unknown/);
  assert.match(failed.form.feedback.textContent, /restoration exchange lost/);
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
