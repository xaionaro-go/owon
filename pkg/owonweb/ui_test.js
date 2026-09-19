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
    append() {},
    replaceChildren() {},
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
    "waveform-header", "command-result", "metric-dmm", "dmm-current-note",
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
  const generatorSubmit = createElement({ type: "submit" });
  const generatorForm = createElement({
    dataset: { api: "/api/generator" },
    elements: [waveform, frequencyHz, load, output, symmetryPercent, generatorSubmit],
    querySelector() { return generatorSubmit; },
  });
  const forms = [channelForm, dmmForm, horizontalForm, triggerForm, generatorForm];
  for (const form of forms) {
    for (const input of form.elements) {
      input.form = form;
      if (input.name) form.elements[input.name] = input;
    }
  }

  return {
    createElement,
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
      if (selector.startsWith("#")) return ids.get(selector.slice(1)) || null;
      return null;
    },
    querySelectorAll(selector) {
      if (selector === "[data-get]") return [dmmRead];
      if (selector === "form[data-api]") return forms;
      if (selector === "[data-measurement]:checked") return measurement.checked ? [measurement] : [];
      return [];
    },
    ids,
    forms,
    controls: { channel, display, offset, scale, relative, autoRange, horizontalOffset, level, dmmFunction, currentType, range, waveform, frequencyHz, load, output, symmetryPercent },
    toastMessages,
    dmmRead,
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

async function startBrowser({ hydrationFails = false, hydrationResponse, respond } = {}) {
  const document = createDocument();
  const window = createElement();
  const fetchCalls = [];
  const fetch = async (url, options = {}) => {
    fetchCalls.push({ url, options });
    if (hydrationFails) throw new Error(`HTTP failed for ${url}`);
    if (hydrationResponse && (url === "/api/device" || url === "/api/state")) return hydrationResponse(url);
    if (respond && url !== "/api/device" && url !== "/api/state") return respond(url, options);
    const body = url === "/api/device" ? { manufacturer: "OWON", model: "test" } : { acquisition: { mode: "ACQUISITION_MODE_SAMPLE" } };
    return { ok: true, status: 200, text: async () => JSON.stringify(body) };
  };
  const schedule = (callback, delay) => {
    const timer = setTimeout(callback, delay);
    timer.unref();
    return timer;
  };
  const context = {
    AbortController,
    EventSource: FakeEventSource,
    Promise,
    TextDecoder,
    atob,
    clearTimeout,
    console,
    document,
    fetch,
    setTimeout: schedule,
    URLSearchParams,
    window,
  };
  vm.runInNewContext(appSource, context, { filename: "app.js" });
  await new Promise((resolve) => setImmediate(resolve));
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(FakeEventSource.instances.length, hydrationResponse ? 0 : 1);
  return { document, window, source: FakeEventSource.instances[0], fetchCalls };
}

async function main() {
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
  for (const [range, label] of [["DMM_RANGE_MV", "mV"], ["DMM_RANGE_V", "V"], ["DMM_RANGE_ON", "ON"], ["DMM_RANGE_UNSPECIFIED", "—"]]) {
    first.source.emit("state", { state: { dmm: { range } } });
    assert.equal(first.document.ids.get("dmm-range-state").textContent, label);
  }
  first.source.emit("state", { state: {} });
  assert.equal(first.document.ids.get("dmm-range-state").textContent, "—");
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
  assert.equal(first.document.ids.get("metric-dmm").textContent, "0");
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
    const browser = await startBrowser({ respond: async () => ({ ok: true, text: async () => JSON.stringify({ value, unit: "V" }) }) });
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
    assert.equal(fetchCalls.length, 2, "restore does not rehydrate or submit drafts");
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
    assert.equal(FakeEventSource.instances.length, replacement === "suspend" ? 0 : 1);
    assert.doesNotMatch(browser.document.ids.get("device-model").textContent, /obsolete hydration/);
    if (replacement === "apply") {
      assert.equal(browser.document.ids.get("device-model").textContent, "OWON new owner");
      assert.equal(browser.document.ids.get("connection-label").textContent, "Live device stream");
    } else {
      browser.window.dispatch("pageshow", { persisted: true });
      assert.equal(FakeEventSource.instances.length, 1);
      assert.notEqual(browser.document.ids.get("connection-label").textContent, "Live device stream");
    }
    assert.equal(browser.fetchCalls.length, 2);
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

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
