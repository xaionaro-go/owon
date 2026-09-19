(() => {
  "use strict";

  const $ = (selector) => document.querySelector(selector);
  const all = (selector) => Array.from(document.querySelectorAll(selector));
  const maxRenderBytes = 8 * 1024 * 1024;
  const state = {
    source: null,
    suspended: false,
    ownerGeneration: 0,
    appliedStreamURL: null,
    lastSequence: null,
    eventSequenceGaps: 0,
    waveformGaps: 0,
    retries: 0,
    terminal: false,
    reconnecting: false,
    stoppedNoticeShown: false,
    hydratedFromStream: false,
    hasRenderedState: false,
    waveform: null,
    command: null,
  };
  let toastTimer;

  function showToast(message, isError = false) {
    clearTimeout(toastTimer);
    const element = $("#toast");
    element.textContent = message;
    element.classList.toggle("error", isError);
    if (message) toastTimer = setTimeout(() => { element.textContent = ""; }, 7000);
  }

  function setConnection(label, connectionState) {
    const banner = $("#connection-banner");
    $("#connection-label").textContent = label;
    $("#connection-dot").className = `status-dot ${connectionState}`;
    banner.className = `connection ${connectionState}`;
    $("#footer-status").textContent = label;
  }

  function setHydration(label, connectionState) {
    const status = $("#hydration-status");
    status.textContent = label;
    status.className = `status-line ${connectionState}`;
  }

  function resetSubscriptionAccounting() {
    state.lastSequence = null;
    state.eventSequenceGaps = 0;
    state.waveformGaps = 0;
    $("#event-sequence").textContent = "—";
    $("#event-gaps").textContent = "0";
    $("#waveform-gaps").textContent = "0";
  }

  function markTouched(event) {
    if (!event.target.matches("[data-optional]")) return;
    event.target.dataset.touched = "true";
    event.target.dataset.revision = String(Number(event.target.dataset.revision || 0) + 1);
    if (event.target.form?.dataset.api === "/api/dmm") updateDMMApplicability(event.target.form);
  }

  function usesCurrentType(value) {
    return value === "DMM_FUNCTION_VOLTAGE" || value === "DMM_FUNCTION_CURRENT";
  }

  function updateDMMApplicability(form) {
    const selection = form.elements.function.value;
    const currentType = form.elements.currentType;
    const incompatible = selection !== "" && !usesCurrentType(selection);
    const conflict = incompatible && currentType.dataset.touched === "true" && currentType.value !== "";
    // Only an acknowledged selection may be reset automatically; explicit conflicting edits stay editable.
    if (incompatible && !conflict) currentType.value = "";
    currentType.disabled = incompatible && !conflict;
    $("#dmm-current-note").textContent = conflict
      ? "Clear Current type (Leave unchanged), or select Voltage or Current before applying."
      : incompatible
        ? "AC/DC does not apply to this function; its previous selection has been cleared."
        : "Voltage and Current require AC/DC. Applying either selection sends the selected pair; these are requested settings, not observed state.";
  }

  function toValue(input) {
    if (input.dataset.fixedValue !== undefined) return JSON.parse(input.dataset.fixedValue);
    if (input.type === "checkbox") return input.checked;
    if (input.dataset.json === "true") return JSON.parse(input.value);
    if (input.value === "") return undefined;
    if (input.name === "offsetDivisions") {
      if (!/^-?\d+$/.test(input.value)) throw new Error("must be whole divisions in decimal notation");
      // ProtoJSON accepts int64 strings; Number would round large offsets before submission.
      const value = BigInt(input.value);
      if (value < -9223372036854775808n || value > 9223372036854775807n) throw new Error("outside the signed 64-bit range");
      if ((input.min && value < BigInt(input.min)) || (input.max && value > BigInt(input.max))) throw new Error(`outside the range [${input.min}, ${input.max}]`);
      return value.toString();
    }
    if (input.type === "number") {
      const value = Number(input.value);
      if (!Number.isFinite(value)) throw new Error(`${input.name} must be a finite number`);
      if (input.name === "symmetryPercent" && (!Number.isInteger(value) || value < 0 || value > 100)) throw new Error("must be a whole percent in [0, 100]");
      return value;
    }
    return input.value;
  }

  function formPayload(form) {
    if (form.dataset.api === "/api/dmm") {
      const currentType = form.elements.currentType;
      if (currentType.dataset.touched === "true" && currentType.value !== "" && !usesCurrentType(form.elements.function.value)) {
        throw new Error("Clear Current type (Leave unchanged), or select Voltage or Current before applying.");
      }
    }
    const payload = {};
    for (const input of form.elements) {
      if (!input.name || (input.disabled && input.dataset.fixedValue === undefined)) continue;
      if (input.hasAttribute("data-optional") && input.dataset.touched !== "true" && input.dataset.fixedValue === undefined) continue;
      try {
        const value = toValue(input);
        if (value !== undefined) payload[input.name] = value;
      } catch (error) {
        throw new Error(`${input.name}: ${error.message}`);
      }
    }
    // Companions come from explicit selections, never inferred device state or previously sent values.
    if (form.dataset.api === "/api/generator" && Object.hasOwn(payload, "frequencyHz")) {
      payload.waveform = form.elements.waveform.value;
      if (!payload.waveform) throw new Error("Select a waveform before applying frequency.");
    }
    if (form.dataset.api === "/api/dmm") {
      if (Object.hasOwn(payload, "currentType")) payload.function = form.elements.function.value;
      if (usesCurrentType(payload.function)) {
        payload.currentType = form.elements.currentType.value;
        if (!payload.currentType) throw new Error("Select AC or DC before applying Voltage or Current.");
      }
    }
    return payload;
  }

  async function requestJSON(url, options = {}) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 15000);
    const headers = { Accept: "application/json", ...(options.headers || {}) };
    if (options.body !== undefined) headers["Content-Type"] = "application/json";
    try {
      const response = await fetch(url, { ...options, headers, signal: controller.signal });
      const text = await response.text();
      let body = {};
      if (text) {
        try { body = JSON.parse(text); } catch (_) { throw new Error("The device returned invalid JSON"); }
      }
      if (!response.ok) throw new Error(body.message || `${body.code || "Request"} failed (${response.status})`);
      return body;
    } catch (error) {
      if (error.name === "AbortError") throw new Error("Request timed out; the device may be offline");
      throw error;
    } finally {
      clearTimeout(timeout);
    }
  }

  function buttonBusy(button, busy) {
    if (!button) return;
    button.disabled = busy;
    if (busy) button.dataset.originalText = button.textContent;
    button.textContent = busy ? "Working…" : (button.dataset.originalText || button.textContent);
  }

  function appliedMessage(result, fallback) {
    const appliedAt = result && result.appliedAt ? ` · applied ${new Date(result.appliedAt).toLocaleTimeString()}` : "";
    return `${fallback}${appliedAt}`;
  }

  async function submitForm(form) {
    const submit = form.querySelector("button[type=submit]");
    if (submit && submit.disabled) return;
    buttonBusy(submit, true);
    try {
      const payload = formPayload(form);
      const submitted = Array.from(form.elements)
        .filter((input) => Object.hasOwn(payload, input.name) && input.dataset.touched === "true")
        .map((input) => ({ input, revision: input.dataset.revision }));
      const result = await requestJSON(form.dataset.api, { method: "POST", body: JSON.stringify(payload) });
      for (const { input, revision } of submitted) {
        if (input.dataset.revision === revision) delete input.dataset.touched;
      }
      if (form.dataset.api === "/api/dmm") updateDMMApplicability(form);
      if (form.dataset.api === "/api/execute") renderCommand(result);
      if (form.dataset.api === "/api/waveform") renderWaveform(result);
      showToast(appliedMessage(result, form.dataset.toast || "Applied"));
    } catch (error) {
      showToast(error.message, true);
    } finally {
      buttonBusy(submit, false);
    }
  }

  function capabilityLabel(value) {
    return String(value || "").replace(/^CAPABILITY_/, "").replaceAll("_", " ");
  }

  function enumLabel(value) {
    const labels = {
      ACQUISITION_MODE_SAMPLE: "Sample",
      ACQUISITION_MODE_PEAK_DETECT: "Peak detect",
      ACQUISITION_MODE_AVERAGE: "Average",
      TRIGGER_SWEEP_AUTO: "Auto",
      TRIGGER_SWEEP_NORMAL: "Normal",
      TRIGGER_SWEEP_SINGLE: "Single",
      DMM_RANGE_ON: "ON",
      DMM_RANGE_MV: "mV",
      DMM_RANGE_V: "V",
      DMM_RANGE_UNSPECIFIED: "—",
    };
    return labels[value] || String(value || "—").replaceAll("_", " ");
  }

  function renderDevice(device) {
    const model = [device.manufacturer || "OWON", device.model || "oscilloscope"].join(" ");
    $("#device-model").textContent = model;
    const identity = [device.serial && `S/N ${device.serial}`, device.firmware && `FW ${device.firmware}`, device.vendorId && `VID ${device.vendorId.toString(16)}`].filter(Boolean).join(" · ");
    $("#device-meta").textContent = identity || "Identity loaded · no additional metadata";
    const list = $("#capability-list");
    list.replaceChildren();
    for (const capability of device.capabilities || []) {
      const tag = document.createElement("span");
      tag.className = "tag";
      tag.textContent = capabilityLabel(capability);
      list.append(tag);
    }
  }

  function renderState(snapshot) {
    state.hasRenderedState = true;
    $("#state-json").textContent = JSON.stringify(snapshot || {}, null, 2);
    if (snapshot && snapshot.capturedAt) $("#last-update").textContent = `Captured ${new Date(snapshot.capturedAt).toLocaleTimeString()}`;
    if (snapshot && snapshot.device) renderDevice(snapshot.device);
    for (const channel of ["ch1", "ch2"]) {
      $(`#metric-${channel}`).textContent = "—";
      $(`#metric-${channel}-unit`).textContent = "";
    }
    for (const measurement of (snapshot && snapshot.measurements) || []) {
      const channel = measurement.channel === "CHANNEL_1" ? "ch1" : measurement.channel === "CHANNEL_2" ? "ch2" : "";
      if (!channel || measurement.kind !== "MEASUREMENT_KIND_PEAK_TO_PEAK") continue;
      $(`#metric-${channel}`).textContent = String(measurement.value ?? 0);
      $(`#metric-${channel}-unit`).textContent = measurement.unit || "";
    }
    $("#metric-acquisition").textContent = snapshot?.acquisition ? enumLabel(snapshot.acquisition.mode) : "—";
    $("#metric-trigger").textContent = snapshot?.trigger ? snapshot.trigger.status || enumLabel(snapshot.trigger.sweep) : "—";
    $("#dmm-range-state").textContent = snapshot?.dmm?.range ? enumLabel(snapshot.dmm.range) : "—";
  }

  function decodeByteLength(base64) {
    if (!base64) return 0;
    const clean = base64.replace(/\s/g, "");
    const padding = clean.endsWith("==") ? 2 : clean.endsWith("=") ? 1 : 0;
    return Math.max(0, Math.floor(clean.length * 3 / 4) - padding);
  }

  function base64Bytes(base64) {
    const binary = atob(base64.replace(/\s/g, ""));
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
    return bytes;
  }

  function downloadBase64(base64, filename, type = "application/octet-stream") {
    if (decodeByteLength(base64) > maxRenderBytes) {
      showToast("The raw payload is too large for a browser download", true);
      return;
    }
    try {
      const link = document.createElement("a");
      link.href = URL.createObjectURL(new Blob([base64Bytes(base64)], { type }));
      link.download = filename;
      link.click();
      setTimeout(() => URL.revokeObjectURL(link.href), 1000);
    } catch (error) {
      showToast(`Download unavailable: ${error.message}`, true);
    }
  }

  function renderCommand(response) {
    const data = response && response.data ? response.data : "";
    const bytes = decodeByteLength(data);
    state.command = bytes > maxRenderBytes ? null : data;
    $("#command-result").textContent = !data ? "Command returned no bytes." : bytes > maxRenderBytes ? `Command returned ${bytes} opaque bytes; base64 is withheld above the browser display limit.` : `base64 (${bytes} bytes):\n${data}`;
    const button = $("#download-command");
    button.disabled = !data || bytes > maxRenderBytes;
    button.title = bytes > maxRenderBytes ? "Payload exceeds the browser download limit" : "Download opaque response bytes";
  }

  function renderHeader(base64) {
    if (!base64 || decodeByteLength(base64) > maxRenderBytes) return "Header is unavailable or exceeds the browser inspection limit.";
    try {
      const text = new TextDecoder().decode(base64Bytes(base64));
      try { return JSON.stringify(JSON.parse(text), null, 2); } catch (_) { return text; }
    } catch (error) {
      return `Header bytes are not valid UTF-8: ${error.message}`;
    }
  }

  function renderWaveform(waveform) {
    const data = waveform && waveform.data ? waveform.data : "";
    const metadata = (waveform && waveform.metadata) || {};
    const bytes = decodeByteLength(data);
    state.waveform = bytes > maxRenderBytes ? { ...waveform, data: "" } : waveform;
    $("#waveform-result").textContent = [
      `Channel: ${waveform && waveform.channel ? waveform.channel : "—"}`,
      `Bytes: ${metadata.dataLengthBytes || bytes}`,
      `Encoding: ${waveform && waveform.encoding ? waveform.encoding : "opaque bytes"}`,
      `Captured: ${waveform && waveform.capturedAt ? waveform.capturedAt : "—"}`,
      data && bytes <= maxRenderBytes ? `base64:\n${data}` : (data ? "base64 omitted from the view because it exceeds the browser limit" : "No waveform bytes returned."),
    ].join("\n");
    $("#waveform-header").textContent = "Header inspection available with the button.";
    $("#download-waveform").disabled = !data || bytes > maxRenderBytes;
    $("#inspect-header").disabled = !waveform || !waveform.screenHeaderJson;
  }

  function updateSequence(sequence) {
    const next = String(sequence);
    const previous = state.lastSequence === null ? null : Number(state.lastSequence);
    const current = Number(next);
    if (previous !== null && Number.isSafeInteger(previous) && Number.isSafeInteger(current) && current > previous + 1) {
      state.eventSequenceGaps += current - previous - 1;
    }
    state.lastSequence = next;
    $("#event-sequence").textContent = next;
    $("#event-gaps").textContent = String(state.eventSequenceGaps);
  }

  function serviceErrorMessage(error) {
    const message = error && error.message ? error.message : "The device stream reported an unknown service error";
    const punctuation = /[.!?]$/.test(message) ? "" : ".";
    const retry = error && error.retryable === true ? " Retrying may recover." : " Retry is not expected to help.";
    return `${message}${punctuation}${retry}`;
  }

  function isCurrentSource(source) {
    return state.source === source && !state.terminal;
  }

  function markReconnecting(source, message) {
    if (!isCurrentSource(source)) return;
    const wasReconnecting = state.reconnecting;
    state.reconnecting = true;
    state.hydratedFromStream = false;
    if (!wasReconnecting) {
      state.retries += 1;
      $("#event-retries").textContent = String(state.retries);
    }
    setConnection("Reconnecting to device…", "degraded");
    setHydration(state.hasRenderedState ? "Retained state is stale · stream reconnecting…" : "Device stream reconnecting…", "degraded");
    if (!wasReconnecting && message) showToast(message, true);
  }

  function markStopped(source, label, message) {
    if (state.source !== source || state.terminal) return;
    state.terminal = true;
    state.reconnecting = false;
    setConnection(label, "error");
    setHydration(state.hasRenderedState ? "Retained state is stale · stream stopped" : "Device stream stopped · retry not expected", "error");
    if (!state.stoppedNoticeShown && message) {
      state.stoppedNoticeShown = true;
      showToast(message, true);
    }
    source.close();
  }

  function handleStateEvent(source, event) {
    updateSequence(event.sequence);
    if (event.state) {
      renderState(event.state);
      state.hydratedFromStream = true;
      setHydration("State recovered from live stream", "success");
      setConnection("Live device stream", "live");
    }
    $("#last-update").textContent = `State event ${new Date().toLocaleTimeString()}`;
  }

  function handleWaveformEvent(source, event) {
    updateSequence(event.sequence);
    if (event.waveform) renderWaveform(event.waveform);
    $("#last-update").textContent = `Waveform event ${new Date().toLocaleTimeString()}`;
  }

  function handleGapEvent(source, event) {
    updateSequence(event.sequence);
    const gap = event.waveformGap || {};
    const count = Number(gap.missingCount);
    if (Number.isSafeInteger(count) && count > 0) state.waveformGaps += count;
    $("#waveform-gaps").textContent = String(state.waveformGaps);
    showToast(`Waveform gap: ${gap.missingCount || "unknown"} capture(s) missing`, true);
  }

  function handleServiceError(source, event) {
    updateSequence(event.sequence);
    const error = event && typeof event === "object" && event.error && typeof event.error === "object" ? event.error : null;
    if (error && error.retryable === true) {
      markReconnecting(source, serviceErrorMessage(error));
      return;
    }
    markStopped(source, "Device stream stopped", serviceErrorMessage(error));
  }

  function handleStreamError(source, event) {
    if (event && typeof event === "object" && event.retryable === true) {
      markReconnecting(source, serviceErrorMessage(event));
      return;
    }
    markStopped(source, "Stream stopped", serviceErrorMessage(event));
  }

  function dispatchStreamEvent(source, event, handler, label) {
    if (!isCurrentSource(source)) return;
    let payload;
    try {
      payload = JSON.parse(event.data);
    } catch (error) {
      markStopped(source, "Stream stopped", `${label} event unreadable: ${error.message}`);
      return;
    }
    if (!isCurrentSource(source)) return;
    if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
      markStopped(source, "Stream stopped", `${label} event must be a JSON object`);
      return;
    }
    handler(source, payload);
  }

  function normalizeStreamOptionsAndBuildURL() {
    const intervalInput = $("#stream-interval");
    const queueInput = $("#stream-queue");
    const interval = Math.min(Number(intervalInput.max), Math.max(Number(intervalInput.min), Number(intervalInput.value) || Number(intervalInput.defaultValue)));
    const queue = Math.min(Number(queueInput.max), Math.max(Number(queueInput.min), Number(queueInput.value) || Number(queueInput.defaultValue)));
    $("#stream-interval").value = interval;
    $("#stream-queue").value = queue;
    const query = new URLSearchParams({ interval: `${interval}ms`, queue_capacity: String(queue), include_controls: String($("#stream-controls").checked), include_screen_header: String($("#stream-header").checked) });
    for (const selector of all("[data-measurement]:checked")) query.append("measurement", selector.dataset.measurement);
    if ($("#stream-wave-ch1").checked) query.append("waveform_channel", "CHANNEL_1");
    if ($("#stream-wave-ch2").checked) query.append("waveform_channel", "CHANNEL_2");
    return `/api/events?${query.toString()}`;
  }

  function connectEvents() {
    if (state.suspended || state.terminal) return;
    state.ownerGeneration += 1;
    if (state.source) state.source.close();
    state.source = null;
    state.reconnecting = false;
    state.stoppedNoticeShown = false;
    state.hydratedFromStream = false;
    state.retries = 0;
    $("#event-retries").textContent = "0";
    resetSubscriptionAccounting();
    setConnection("Connecting stream · awaiting fresh state", "pending");
    setHydration(state.hasRenderedState ? "Retained state is stale · awaiting fresh state" : "Connecting · awaiting fresh state", "pending");
    const source = new EventSource(state.appliedStreamURL);
    state.source = source;
    source.onopen = () => {
      if (!isCurrentSource(source)) return;
      resetSubscriptionAccounting();
      state.terminal = false;
      state.reconnecting = false;
      state.stoppedNoticeShown = false;
      state.hydratedFromStream = false;
      setConnection("Stream connected · awaiting fresh state", "pending");
      setHydration(state.hasRenderedState ? "Retained state · awaiting fresh state" : "Connected · awaiting fresh state", "pending");
    };
    source.addEventListener("state", (event) => {
      dispatchStreamEvent(source, event, handleStateEvent, "State");
    });
    source.addEventListener("waveform", (event) => {
      dispatchStreamEvent(source, event, handleWaveformEvent, "Waveform");
    });
    source.addEventListener("waveform-gap", (event) => {
      dispatchStreamEvent(source, event, handleGapEvent, "Gap");
    });
    source.addEventListener("service-error", (event) => {
      dispatchStreamEvent(source, event, handleServiceError, "Service error");
    });
    source.addEventListener("stream-error", (event) => {
      dispatchStreamEvent(source, event, handleStreamError, "Stream error");
    });
    source.onerror = () => {
      if (!isCurrentSource(source)) return;
      if (source.readyState === EventSource.CONNECTING) {
        markReconnecting(source, "Device stream interrupted; retrying automatically.");
        return;
      }
      if (source.readyState === EventSource.CLOSED) {
        markStopped(source, "Stream stopped", "The device stream closed and will not retry.");
      }
    };
  }

  async function hydrate() {
    const generation = state.ownerGeneration;
    state.hydratedFromStream = false;
    state.hasRenderedState = false;
    setHydration("Loading device identity and complete state…", "pending");
    const results = await Promise.allSettled([requestJSON("/api/device"), requestJSON("/api/state")]);
    // A replacement subscription or page suspension owns all later rendering and startup.
    if (generation !== state.ownerGeneration || state.suspended) return;
    const [device, snapshot] = results;
    if (device.status === "fulfilled") renderDevice(device.value);
    if (snapshot.status === "fulfilled") renderState(snapshot.value);
    const failures = results.filter((result) => result.status === "rejected");
    if (state.hydratedFromStream) {
      setHydration("State recovered from live stream", "success");
    } else if (failures.length === 0) {
      setHydration("Identity and state loaded", "success");
    } else if (failures.length < results.length) {
      setHydration("Partial state loaded · retrying through stream", "degraded");
      showToast(failures[0].reason.message, true);
    } else {
      setHydration("Device unavailable · stream will retry", "error");
      showToast(failures[0].reason.message, true);
    }
    connectEvents();
  }

  async function refreshDMM() {
    const button = $("[data-get]");
    buttonBusy(button, true);
    try {
      const result = await requestJSON("/api/dmm/measurement");
      $("#metric-dmm").textContent = result == null ? "—" : `${result.value ?? 0} ${result.unit || ""}`.trim();
      showToast(`DMM reading refreshed${result.capturedAt ? ` · ${new Date(result.capturedAt).toLocaleTimeString()}` : ""}`);
    } catch (error) {
      showToast(error.message, true);
    } finally {
      buttonBusy(button, false);
    }
  }

  async function action(button) {
    buttonBusy(button, true);
    try {
      const result = await requestJSON(button.dataset.action, { method: "POST", body: "" });
      showToast(appliedMessage(result, `${button.dataset.action.replace("/api/", "")} applied`));
    } catch (error) {
      showToast(error.message, true);
    } finally {
      buttonBusy(button, false);
    }
  }

  document.addEventListener("change", markTouched);
  document.addEventListener("input", markTouched);
  updateDMMApplicability($('form[data-api="/api/dmm"]'));
  all("form[data-api]").forEach((form) => form.addEventListener("submit", (event) => {
    event.preventDefault();
    submitForm(form);
  }));
  all("[data-action]").forEach((button) => button.addEventListener("click", () => action(button)));
  all("[data-get]").forEach((button) => button.addEventListener("click", refreshDMM));
  $("#stream-apply").addEventListener("click", () => {
    state.appliedStreamURL = normalizeStreamOptionsAndBuildURL();
    state.terminal = false;
    connectEvents();
    showToast("Subscribe options applied");
  });
  $("#download-command").addEventListener("click", () => downloadBase64(state.command, "owon-response.bin"));
  $("#download-waveform").addEventListener("click", () => downloadBase64(state.waveform && state.waveform.data, "owon-waveform.bin"));
  $("#inspect-header").addEventListener("click", () => {
    $("#waveform-header").textContent = renderHeader(state.waveform && state.waveform.screenHeaderJson);
  });
  window.addEventListener("pagehide", () => {
    state.suspended = true;
    state.ownerGeneration += 1;
    state.hydratedFromStream = false;
    if (state.source) state.source.close();
    state.source = null;
    setConnection(state.terminal ? "Stream stopped" : "Page suspended · stream paused", state.terminal ? "error" : "degraded");
    setHydration(state.hasRenderedState ? "Retained state is stale · page suspended" : "Page suspended · awaiting fresh state", "degraded");
  });
  window.addEventListener("pageshow", (event) => {
    if (!event.persisted || !state.suspended) return;
    state.suspended = false;
    if (state.terminal) {
      setHydration(state.hasRenderedState ? "Retained state is stale · stream stopped" : "Device stream stopped · apply to retry", "error");
      return;
    }
    connectEvents();
  });
  state.appliedStreamURL = normalizeStreamOptionsAndBuildURL();
  hydrate();
})();
