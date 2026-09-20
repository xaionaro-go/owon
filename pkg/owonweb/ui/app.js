(() => {
  "use strict";

  const $ = (selector) => document.querySelector(selector);
  const all = (selector) => Array.from(document.querySelectorAll(selector));
  const maxRenderBytes = 8 * 1024 * 1024;
  const state = {
    source: null,
    suspended: false,
    ownerGeneration: 0,
    streamRevision: 0,
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
    command: null,
  };
  const scope = {
    slots: { CHANNEL_1: { capture: null, sequence: null, stale: "Awaiting capture" }, CHANNEL_2: { capture: null, sequence: null, stale: "Awaiting capture" } },
    epoch: 0, interval: 1000, channels: [], timer: null, groups: {}, cursorPanels: new Map(), cursorViews: new Map(), lastCursorKeys: new Set(),
    manual: { capture: null, stale: "", generation: 0, controller: null },
  };
  const dmm = { active: false, timer: null, controller: null, generation: 0, captured: "", latest: null, stale: false, error: "", hostHold: false, configuring: false };
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
    if (event.target.form?.dataset.api === "/api/generator") {
      if (!generatorPending(event.target)) {
        delete event.target.dataset.touched;
        delete event.target.dataset.sent;
      }
      renderGeneratorDrafts(event.target.form);
    }
  }

  const generatorWaveforms = {
    GENERATOR_WAVEFORM_SINE: { maximum: 25e6 },
    GENERATOR_WAVEFORM_SQUARE: { maximum: 5e6 },
    GENERATOR_WAVEFORM_RAMP: { maximum: 1e6 },
    GENERATOR_WAVEFORM_PULSE: { maximum: 5e6 },
    GENERATOR_WAVEFORM_AMP_ALT: { maximum: 5e6, builtin: true },
    GENERATOR_WAVEFORM_ATT_ALT: { maximum: 5e6, builtin: true },
    GENERATOR_WAVEFORM_STAIR_DOWN: { maximum: 5e6, builtin: true },
    GENERATOR_WAVEFORM_STAIR_UP_DOWN: { maximum: 5e6, builtin: true },
    GENERATOR_WAVEFORM_STAIR_UP: { maximum: 5e6, builtin: true },
    GENERATOR_WAVEFORM_BESSEL_J: { maximum: 5e6, builtin: true },
    GENERATOR_WAVEFORM_BESSEL_Y: { maximum: 5e6, builtin: true },
    GENERATOR_WAVEFORM_SINC: { maximum: 5e6, builtin: true },
  };
  const generatorShapeFields = ["symmetryPercent", "dutyPercent", "pulseWidthSeconds", "risingSeconds", "fallingSeconds"];
  const generatorTimingFields = ["frequencyHz", "periodSeconds", "pulseWidthSeconds", "risingSeconds", "fallingSeconds"];

  function generatorPending(input) {
    return !!input && input.dataset.touched === "true" && (input.type === "checkbox" || input.validity?.badInput || input.value !== "");
  }

  function generatorCandidate(form) {
    const payload = {}, errors = {};
    for (const input of form.elements) {
      if (!input.name || !generatorPending(input)) continue;
      try {
        if (input.validity?.badInput) throw new Error("Enter a complete, finite number.");
        const value = toValue(input);
        if (generatorTimingFields.includes(input.name) && value <= 0) throw new Error("Enter a positive number.");
        if (input.name === "dutyPercent" && (value < 0 || value > 100)) throw new Error("Duty must be in [0, 100].");
        if (input.name === "load" && !["GENERATOR_LOAD_ON", "GENERATOR_LOAD_OFF"].includes(value)) throw new Error("Select ON or OFF.");
        payload[input.name] = value;
      } catch (error) { errors[input.name] = error.message; }
    }
    const needsWaveform = ["frequencyHz", "periodSeconds", ...generatorShapeFields].some(name => generatorPending(form.elements[name]));
    if (needsWaveform) payload.waveform = form.elements.waveform.value;
    const waveform = Object.hasOwn(generatorWaveforms, payload.waveform) ? generatorWaveforms[payload.waveform] : null;
    if ((needsWaveform || Object.hasOwn(payload, "waveform")) && !waveform) errors.waveform = "Select a waveform before applying timing or shape settings.";
    if (generatorPending(form.elements.frequencyHz) && generatorPending(form.elements.periodSeconds)) errors.periodSeconds = "Choose frequency or period, not both. Clear one pending value.";
    if (waveform) {
      if (Object.hasOwn(payload, "frequencyHz") && (payload.frequencyHz < 0.1 || payload.frequencyHz > waveform.maximum)) errors.frequencyHz = `Frequency must be in [0.1, ${waveform.maximum}] Hz for this waveform.`;
      if (Object.hasOwn(payload, "periodSeconds") && (payload.periodSeconds < 1 / waveform.maximum || payload.periodSeconds > 10)) errors.periodSeconds = `Period must be in [${1 / waveform.maximum}, 10] s for this waveform.`;
      if (waveform.builtin) for (const name of generatorShapeFields) {
        if (Object.hasOwn(payload, name)) errors[name] = "Built-in shape modifiers are unsupported. Clear this pending value or choose a standard waveform.";
      }
    }
    return { payload, errors };
  }

  function renderGeneratorDrafts(form, errors = generatorCandidate(form).errors) {
    for (const input of form.elements) {
      if (!input.name) continue;
      const message = form.dataset.validated === "true" ? errors[input.name] || "" : "";
      $(`#generator-${input.name}-error`).textContent = message;
      if (message) input.setAttribute("aria-invalid", "true");
      else input.removeAttribute("aria-invalid");
      $(`#generator-${input.name}-pending`).textContent = generatorPending(input) ? "Pending edit" : input.dataset.sent === "true" ? "Previously sent request" : "";
    }
  }

  async function submitGenerator(form) {
    const submit = form.querySelector("button[type=submit]");
    if (submit.disabled) return;
    const { payload, errors } = generatorCandidate(form);
    form.dataset.validated = "true";
    renderGeneratorDrafts(form, errors);
    const invalid = Array.from(form.elements).find(input => errors[input.name]);
    if (invalid) {
      const message = `${Object.values(errors).join(" ")} No request sent.`;
      setFormStatus(form, message, "error");
      showToast(message, true);
      invalid.focus({ preventScroll: true });
      $(`#generator-${invalid.name}-field`).scrollIntoView({ block: "center", behavior: "instant" });
      return;
    }
    if (!Object.keys(payload).length) {
      setFormStatus(form, "No pending changes. No request sent.", "pending");
      return;
    }
    const submitted = Array.from(form.elements)
      .filter(input => Object.hasOwn(payload, input.name) && generatorPending(input))
      .map(input => ({ input, revision: input.dataset.revision }));
    const body = JSON.stringify(payload);
    buttonBusy(submit, true);
    form.setAttribute("aria-busy", "true");
    setFormStatus(form, "Sending write request…", "pending");
    try {
      const result = await requestJSON(form.dataset.api, { method: "POST", body });
      for (const { input, revision } of submitted) {
        if (input.dataset.revision !== revision) continue;
        delete input.dataset.touched;
        input.dataset.sent = "true";
      }
      const newer = Array.from(form.elements).some(generatorPending);
      setFormStatus(form, `${generatorResultMessage(result)}${newer ? " Newer edits remain unsent." : ""}`, "success");
    } catch (error) {
      const message = error.generator
        ? `Request failed; ${generatorResultMessage({ generator: error.generator })} Review device before retrying. ${error.message}`
        : `Request failed; delivery may be partial or unknown. Review device before retrying. ${error.message}`;
      setFormStatus(form, message, "error");
      showToast(message, true);
    } finally {
      renderGeneratorDrafts(form);
      buttonBusy(submit, false);
      form.setAttribute("aria-busy", "false");
    }
  }

  function usesCurrentType(value) {
    return value === "DMM_FUNCTION_VOLTAGE" || value === "DMM_FUNCTION_CURRENT";
  }

  function dmmCompanionError(form) {
    const currentType = form.elements.currentType;
    const selection = form.elements.function;
    if (currentType.dataset.touched === "true" && currentType.value !== "" && !usesCurrentType(selection.value)) {
      return "Clear Current type (Leave unchanged), or select Voltage or Current before applying.";
    }
    if (selection.dataset.touched === "true" && usesCurrentType(selection.value) && !currentType.value) {
      return "Select AC or DC before applying Voltage or Current.";
    }
    return "";
  }

  function updateDMMApplicability(form) {
    const selection = form.elements.function.value;
    const currentType = form.elements.currentType;
    const incompatible = selection !== "" && !usesCurrentType(selection);
    const conflict = incompatible && currentType.dataset.touched === "true" && currentType.value !== "";
    // Only an acknowledged selection may be reset automatically; explicit conflicting edits stay editable.
    if (incompatible && !conflict) currentType.value = "";
    currentType.disabled = incompatible && !conflict;
    if (currentType.getAttribute("aria-invalid") === "true") {
      const message = dmmCompanionError(form);
      $("#dmm-current-error").textContent = message;
      if (message) setFormStatus(form, message, "error");
      else {
        currentType.removeAttribute("aria-invalid");
        setFormStatus(form, "Selection corrected · ready to apply", "pending");
      }
    }
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
      const message = dmmCompanionError(form);
      if (message) {
        $("#dmm-current-error").textContent = message;
        form.elements.currentType.setAttribute("aria-invalid", "true");
        form.elements.currentType.focus({ preventScroll: true });
        $("#dmm-current-field").scrollIntoView({ block: "center", behavior: "instant" });
        throw new Error(message);
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
    const abort = () => controller.abort();
    options.signal?.addEventListener("abort", abort, { once: true });
    if (options.signal?.aborted) controller.abort();
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
      if (!response.ok) {
        const requestError = new Error(body.message || `${body.code || "Request"} failed (${response.status})`);
        if (body && typeof body === "object") Object.assign(requestError, body);
        throw requestError;
      }
      return body;
    } catch (error) {
      if (error.name === "AbortError") throw new Error("Request timed out; the device may be offline");
      throw error;
    } finally {
      clearTimeout(timeout);
      options.signal?.removeEventListener("abort", abort);
    }
  }

  function buttonBusy(button, busy) {
    if (!button) return;
    button.disabled = busy;
    button.setAttribute("aria-busy", String(busy));
    if (busy) button.dataset.originalText = button.textContent;
    button.textContent = busy ? "Working…" : (button.dataset.originalText || button.textContent);
  }

  function appliedMessage(result, fallback) {
    const appliedAt = result && result.appliedAt ? ` · applied ${new Date(result.appliedAt).toLocaleTimeString()}` : "";
    return `${fallback}${appliedAt}`;
  }

  function setFormStatus(form, message, status) {
    const feedback = form.querySelector("[data-form-status]");
    feedback.textContent = message;
    feedback.className = `form-status status-line ${status}`;
  }

  function generatorLogicalReadback(operation) {
    return operation?.observation?.context?.match === "GENERATOR_CONTEXT_MATCH" &&
      operation?.observation?.output?.status === "GENERATOR_OBSERVATION_OBSERVED";
  }

  function generatorResultMessage(result) {
    const operation = result?.generator;
    const logicalReadback = generatorLogicalReadback(operation);
    switch (operation?.delivery) {
      case "GENERATOR_DELIVERY_TRANSPORT_COMPLETE_READBACK_VERIFIED":
        return logicalReadback
          ? "Transport complete; logical waveform/output readback verified (not electrical validation)."
          : "Transport complete; no logical generator readback was captured (not electrical validation).";
      case "GENERATOR_DELIVERY_TRANSPORT_COMPLETE_READBACK_UNVERIFIED":
        return logicalReadback
          ? "Transport complete; logical waveform/output readback verified, numeric readback unverified."
          : "Transport complete; no logical generator readback was captured; numeric readback unverified.";
      case "GENERATOR_DELIVERY_NOT_ATTEMPTED":
        return "No generator mutation was attempted; readback preflight did not converge.";
      case "GENERATOR_DELIVERY_PARTIAL_OR_UNKNOWN":
        return "Generator delivery is partial or unknown; review logical readback before retrying.";
      default:
        return "Write request acknowledged; device readback unavailable.";
    }
  }

  async function submitForm(form) {
    if (form.dataset.api === "/api/generator") return submitGenerator(form);
    const submit = form.querySelector("button[type=submit]");
    if (submit && submit.disabled) return;
    buttonBusy(submit, true);
    form.setAttribute("aria-busy", "true");
    setFormStatus(form, "Sending request…", "pending");
    const manual = form.dataset.api === "/api/waveform";
    const generation = manual ? ++scope.manual.generation : null;
    const controller = manual ? new AbortController() : null;
    if (manual) scope.manual.controller = controller;
    try {
      const payload = formPayload(form);
      const submitted = Array.from(form.elements)
        .filter((input) => Object.hasOwn(payload, input.name) && input.dataset.touched === "true")
        .map((input) => ({ input, revision: input.dataset.revision }));
      if (form.dataset.api === "/api/dmm") {
        dmm.configuring = true;
        pauseDMM("Settings request in progress");
      }
      const result = await requestJSON(form.dataset.api, { method: "POST", body: JSON.stringify(payload), ...(manual ? { signal: controller.signal } : {}) });
      if (manual && generation !== scope.manual.generation) return;
      for (const { input, revision } of submitted) {
        if (input.dataset.revision === revision) delete input.dataset.touched;
      }
      if (form.dataset.api === "/api/dmm") updateDMMApplicability(form);
      if (form.dataset.api === "/api/execute") renderCommand(result);
      if (manual) {
        scope.manual.capture = admitCapture(result, `manual-${generation}`);
        scope.manual.stale = "";
        $("#raw-source").value = "manual";
        renderRawSelection();
      }
      setFormStatus(form, "Request acknowledged · see observed readouts for device state", "success");
    } catch (error) {
      if (manual && generation !== scope.manual.generation) return;
      if (manual) { scope.manual.stale = error.message; renderRawSelection(); }
      setFormStatus(form, error.message, "error");
      showToast(error.message, true);
    } finally {
      if (manual && scope.manual.controller === controller) scope.manual.controller = null;
      buttonBusy(submit, false);
      form.setAttribute("aria-busy", "false");
      if (form.dataset.api === "/api/dmm") {
        dmm.configuring = false;
        resumeDMM();
      }
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

  function observedEnum(value) {
    if (!value || /_UNSPECIFIED$/.test(value)) return "unavailable";
    return enumLabel(value).replace(/^(COUPLING|TRIGGER SOURCE|TRIGGER SLOPE) /, "")
      .replace("CHANNEL 1", "Channel 1").replace("CHANNEL 2", "Channel 2")
      .replace("RISING", "Rising").replace("FALLING", "Falling");
  }

  function dmmUnit(value) {
    return typeof value === "string" && value.trim() ? value : "(unit unknown)";
  }

  function dmmRangeLabel(value) {
    const range = value?.range;
    if (range && range !== "DMM_RANGE_UNSPECIFIED") return enumLabel(range);
    const raw = value?.observedRangeToken;
    return typeof raw === "string" && raw.trim() ? `Unknown (raw: ${raw.trim()})` : "Range unavailable";
  }

  function renderObserved(snapshot) {
    const rawOffset = value => `Offset (raw screen header, unit unknown): ${value ?? "unavailable"}`;
    for (const [index, id] of [[1, "ch1"], [2, "ch2"]]) {
      const channel = snapshot?.channels?.find(value => value.channel === `CHANNEL_${index}`);
      $(`#observed-${id}`).textContent = channel
        ? `Observed · Display: ${channel.display ? "On" : "Off"} · Coupling: ${observedEnum(channel.coupling)} · Probe: ${channel.probeAttenuation > 0 ? `${channel.probeAttenuation}×` : "unavailable"} · Scale: ${channel.scale || "unavailable"} · ${rawOffset(channel.screenHeaderOffset)}`
        : "Observed channel state unavailable";
    }
    const acquisition = snapshot?.acquisition;
    $("#observed-acquisition").textContent = acquisition
      ? `Observed · Mode: ${observedEnum(acquisition.mode)} · Depth: ${acquisition.memoryDepth || "unavailable"}` : "Observed acquisition state unavailable";
    const horizontal = snapshot?.horizontal;
    $("#observed-horizontal").textContent = horizontal
      ? `Observed · Scale: ${horizontal.scale || "unavailable"} · ${rawOffset(horizontal.screenHeaderOffset)}` : "Observed horizontal state unavailable";
    const trigger = snapshot?.trigger;
    $("#observed-trigger").textContent = trigger
      ? `Observed · Source: ${observedEnum(trigger.source)} · Slope: ${observedEnum(trigger.slope)} · Coupling: ${observedEnum(trigger.coupling)} · Level: ${trigger.level ?? 0} V · Sweep: ${observedEnum(trigger.sweep)} · Status: ${trigger.status || "unavailable"}` : "Observed trigger state unavailable";
    const measurements = $("#measurements");
    measurements.replaceChildren();
    for (const measurement of snapshot?.measurements || []) {
      const item = document.createElement("li");
      const channel = measurement.channel === "CHANNEL_1" ? "Channel 1" : measurement.channel === "CHANNEL_2" ? "Channel 2" : "Channel unavailable";
      const kind = measurement.kind ? measurement.kind.replace(/^MEASUREMENT_KIND_/, "").replaceAll("_", " ").toLowerCase() : "kind unavailable";
      item.textContent = `${channel} · ${kind} · ${measurement.value ?? 0} ${dmmUnit(measurement.unit)}`;
      measurements.append(item);
    }
    if (!measurements.children.length) {
      const item = document.createElement("li");
      item.textContent = "No measurements returned · awaiting selected readings";
      measurements.append(item);
    }
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
    renderObserved(snapshot);
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
      $(`#metric-${channel}-unit`).textContent = dmmUnit(measurement.unit);
    }
    $("#metric-acquisition").textContent = snapshot?.acquisition ? enumLabel(snapshot.acquisition.mode) : "—";
    $("#metric-trigger").textContent = snapshot?.trigger ? snapshot.trigger.status || enumLabel(snapshot.trigger.sweep) : "—";
    $("#dmm-range-state").textContent = dmmRangeLabel(snapshot?.dmm);
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

  function downloadText(text, filename, type) {
    if (typeof text !== "string" || text.length > maxRenderBytes * 2) {
      showToast("The local export is too large for a browser download", true);
      return;
    }
    try {
      const link = document.createElement("a");
      link.href = URL.createObjectURL(new Blob([text], { type }));
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

  function captureSequence(value) {
    if (typeof value !== "string" || !/^\d+$/.test(value) || value.length > 20) return null;
    const sequence = BigInt(value);
    return sequence <= 18446744073709551615n ? sequence : null;
  }

  function boundedCaptureBytes(value, label, issues) {
    if (!value) return "";
    if (typeof value !== "string") { issues.push(`${label}: invalid base64`); return ""; }
    if (value.length > Math.ceil(maxRenderBytes / 3) * 4 || decodeByteLength(value) > maxRenderBytes) {
      issues.push(`${label}: browser byte limit (8 MiB)`); return "";
    }
    if (value.length % 4 || !/^[A-Za-z0-9+/]*={0,2}$/.test(value)) { issues.push(`${label}: invalid base64`); return ""; }
    return value;
  }

  function admitCapture(waveform, identity) {
    if (!waveform || !Object.hasOwn(scope.slots, waveform.channel)) throw new Error("Render error: unsupported waveform channel");
    const issues = [];
    const capture = {
      channel: waveform.channel, identity,
      data: boundedCaptureBytes(waveform.data, "Raw data", issues),
      header: boundedCaptureBytes(waveform.screenHeaderJson, "Header", issues),
      start: waveform.captureStartedAt, end: waveform.capturedAt, trace: null, issue: "",
    };
    capture.byteLength = decodeByteLength(capture.data);
    const trace = waveform.screenTrace;
    const reason = typeof waveform.screenTraceUnavailableReason === "string" ? waveform.screenTraceUnavailableReason.trim() : "";
    const positive = value => Number.isInteger(value) && value > 0 && value <= 4294967295;
    const signed = value => Number.isInteger(value) && value >= -2147483648 && value <= 2147483647;
    if (!isCaptureTimestamp(capture.start) || !isCaptureTimestamp(capture.end)) capture.issue = "Render error: invalid host capture timestamps";
    else if (!!trace === !!reason) capture.issue = "Render error: expected exactly one trace or unavailable reason";
    else if (reason) capture.issue = waveform.screenTraceUnavailableReason;
    else if (typeof trace !== "object" || Array.isArray(trace) || !positive(trace.width) || !positive(trace.height)
      || !positive(trace.horizontalDivisions) || !positive(trace.verticalDivisions) || typeof trace.profile !== "string" || !trace.profile.trim()
      || !Array.isArray(trace.y) || trace.y.length !== trace.width) capture.issue = "Render error: malformed screen geometry";
    else if (trace.width > 16384 || trace.horizontalDivisions > 128 || trace.verticalDivisions > 128) capture.issue = "Browser plot limit: 16,384 points and 128 divisions per axis";
    else if (!trace.y.every(signed) || !signed(Object.hasOwn(trace, "groundY") ? trace.groundY : 0)) capture.issue = "Render error: screen ordinates must be signed 32-bit integers";
    else capture.trace = { profile: trace.profile, width: trace.width, height: trace.height, horizontalDivisions: trace.horizontalDivisions, verticalDivisions: trace.verticalDivisions, y: trace.y, groundY: trace.groundY ?? 0 };
    capture.byteIssues = issues.join(" · ");
    capture.label = `${capture.channel} · ${identity} · host I/O start ${isCaptureTimestamp(capture.start) ? capture.start : "unavailable"} · end ${isCaptureTimestamp(capture.end) ? capture.end : "unavailable"}`;
    return capture;
  }

  function selectedCapture() {
    return $("#raw-source").value === "manual" ? scope.manual : scope.slots[$("#raw-source").value];
  }

  function renderRawSelection() {
    const slot = selectedCapture();
    const capture = slot?.capture;
    $("#waveform-result").textContent = capture
      ? `${$("#raw-source").value === "manual" ? "Manual snapshot" : "Streamed capture"} · ${capture.label}\n${slot.stale ? `Stale · ${slot.stale}\n` : ""}${capture.issue ? `${capture.issue}\n` : ""}${capture.byteIssues ? `${capture.byteIssues}\n` : ""}Raw bytes: ${capture.byteLength} · inspect or download explicitly`
      : "No capture available for this source.";
    $("#download-waveform").disabled = !capture?.data;
    $("#download-trace-csv").disabled = !capture?.trace || !!capture.issue;
    $("#download-trace-svg").disabled = !capture?.trace || !!capture.issue;
    $("#inspect-waveform").disabled = !capture?.data;
    $("#inspect-header").disabled = !capture?.header;
  }

  function traceCSV(capture) {
    const trace = capture.trace;
    const rows = [
      "# OWON browser-local screen trace; units are screen slots/counts",
      `# profile=${trace.profile},width=${trace.width},height=${trace.height},ground_y=${trace.groundY}`,
      "x_screen,y_screen,ground_y",
    ];
    for (let x = 0; x < trace.y.length; x += 1) rows.push(`${x},${trace.y[x]},${trace.groundY}`);
    return `${rows.join("\n")}\n`;
  }

  function xmlEscape(value) {
    return String(value).replace(/[&<>\"']/g, character => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&apos;" }[character]));
  }

  function traceSVG(capture) {
    const trace = capture.trace;
    const path = trace.y.map((y, x) => `${x ? "L" : "M"}${x} ${y}`).join(" ");
    const size = Math.min(trace.width, trace.height) / 40;
    const points = capture.channel === "CHANNEL_1"
      ? `0,${trace.groundY - size} ${size * 2},${trace.groundY} 0,${trace.groundY + size}`
      : `0,${trace.groundY} ${size},${trace.groundY - size} ${size * 2},${trace.groundY} ${size},${trace.groundY + size}`;
    const stroke = capture.channel === "CHANNEL_1" ? "#ffd36b" : "#79d8ff";
    const dash = capture.channel === "CHANNEL_1" ? "" : " stroke-dasharray=\"6 3\"";
    return `<?xml version="1.0" encoding="UTF-8"?>\n<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${trace.width} ${trace.height}" role="img" aria-label="${xmlEscape(capture.channel)} vendor screen trace"><title>${xmlEscape(capture.label)}</title><rect width="100%" height="100%" fill="#04101a"/><path d="${path}" fill="none" stroke="${stroke}" stroke-width="1.5" vector-effect="non-scaling-stroke"${dash}/><polygon points="${points}" fill="${stroke}"/></svg>\n`;
  }

  function svgElement(name, attributes = {}) {
    const element = document.createElementNS("http://www.w3.org/2000/svg", name);
    for (const [key, value] of Object.entries(attributes)) element.setAttribute(key, String(value));
    return element;
  }

  function cursorIdentity(trace) {
    return JSON.stringify([trace.profile, trace.width, trace.height, trace.horizontalDivisions, trace.verticalDivisions]);
  }

  function cursorPanel(trace, previousKeys) {
    const key = cursorIdentity(trace);
    let panel = scope.cursorPanels.get(key);
    if (panel) return panel;
    panel = {
      key,
      profile: trace.profile,
      width: trace.width,
      height: trace.height,
      horizontalDivisions: trace.horizontalDivisions,
      verticalDivisions: trace.verticalDivisions,
      points: { A: null, B: null },
      active: "A",
      geometryNotice: previousKeys.size ? "Screen geometry changed · cursors reset" : "",
    };
    scope.cursorPanels.set(key, panel);
    return panel;
  }

  function clampCursorPoint(panel, point) {
    return {
      x: Math.min(panel.width - 1, Math.max(0, Math.round(point.x))),
      y: Math.min(panel.height, Math.max(0, Math.round(point.y))),
    };
  }

  function signedCursorDelta(value) {
    return `${value >= 0 ? "+" : ""}${value}`;
  }

  function cursorPointLabel(point) {
    return point ? `(x ${point.x} screen slots, y ${point.y} screen counts)` : "(unset)";
  }

  function cursorReadout(panel) {
    const a = panel.points.A;
    const b = panel.points.B;
    const delta = a && b
      ? `(x ${signedCursorDelta(b.x - a.x)} screen slots, y ${signedCursorDelta(b.y - a.y)} screen counts; x right-positive, y down-positive)`
      : `(x unknown screen slots, y unknown screen counts; x right-positive, y down-positive)`;
    return `A=${cursorPointLabel(a)} · B=${cursorPointLabel(b)} · ΔB-A=${delta}`;
  }

  function removeCursorMarkers(view) {
    if (!view.markerGroup) return;
    if (typeof view.markerGroup.remove === "function") view.markerGroup.remove();
    else {
      const index = view.svg.children.indexOf(view.markerGroup);
      if (index >= 0) view.svg.children.splice(index, 1);
    }
    view.markerGroup = null;
  }

  function renderCursorMarkers(view) {
    removeCursorMarkers(view);
    const points = Object.entries(view.panel.points).filter(([, point]) => point);
    if (!points.length) return;
    const markers = svgElement("g", { class: "scope-cursor-markers", "aria-hidden": "true" });
    for (const [name, point] of points) {
      const marker = svgElement("g", { class: `scope-cursor-marker scope-cursor-${name.toLowerCase()}`, "data-cursor": name });
      marker.append(
        svgElement("line", { x1: point.x, x2: point.x, y1: 0, y2: view.panel.height, class: "scope-cursor-line" }),
        svgElement("line", { x1: 0, x2: view.panel.width, y1: point.y, y2: point.y, class: "scope-cursor-line" }),
        svgElement("circle", { cx: point.x, cy: point.y, r: Math.max(2, Math.min(view.panel.width, view.panel.height) / 45), class: "scope-cursor-point" }),
      );
      markers.append(marker);
    }
    view.markerGroup = markers;
    view.svg.append(markers);
  }

  function renderCursorView(view) {
    view.readout.textContent = cursorReadout(view.panel);
    const stale = view.channels.map(channel => scope.slots[channel]?.stale).find(Boolean);
    const freshness = stale ? `Stale · ${stale}` : "Fresh screen trace";
    view.status.textContent = `${freshness}${view.panel.geometryNotice ? ` · ${view.panel.geometryNotice}` : ""}`;
    for (const name of ["A", "B"]) {
      const button = view.buttons[name];
      button.setAttribute("aria-pressed", String(view.panel.active === name));
    }
    renderCursorMarkers(view);
  }

  function setCursorActive(view, name) {
    view.panel.active = name;
    renderCursorView(view);
  }

  function setCursorPoint(view, point) {
    view.panel.points[view.panel.active] = clampCursorPoint(view.panel, point);
    renderCursorView(view);
  }

  function pointerClientPosition(event) {
    const touch = event.touches?.[0] || event.changedTouches?.[0];
    const clientX = Number.isFinite(event.clientX) ? event.clientX : touch?.clientX;
    const clientY = Number.isFinite(event.clientY) ? event.clientY : touch?.clientY;
    return Number.isFinite(clientX) && Number.isFinite(clientY) ? { clientX, clientY } : null;
  }

  function pointerCursorPoint(event, svg, panel) {
    const client = pointerClientPosition(event);
    const rect = svg.getBoundingClientRect?.();
    if (!client || !rect) return null;
    const rectWidth = Number(rect.width || (rect.right - rect.left));
    const rectHeight = Number(rect.height || (rect.bottom - rect.top));
    if (!Number.isFinite(rectWidth) || !Number.isFinite(rectHeight) || rectWidth <= 0 || rectHeight <= 0) return null;
    let x = (client.clientX - Number(rect.left || 0)) * panel.width / rectWidth;
    let y = (client.clientY - Number(rect.top || 0)) * panel.height / rectHeight;
    if (svg.getScreenCTM && svg.createSVGPoint) {
      try {
        const transform = svg.getScreenCTM();
        if (transform?.inverse) {
          const point = svg.createSVGPoint();
          point.x = client.clientX;
          point.y = client.clientY;
          const local = point.matrixTransform(transform.inverse());
          x = local.x;
          y = local.y;
        }
      } catch (_) {
        // Some browsers expose an unavailable CTM while an SVG is being resized.
        // The client-rectangle mapping above remains deterministic and bounded.
      }
    }
    return clampCursorPoint(panel, { x, y });
  }

  function cursorKeyboardPoint(event, view) {
    const movement = {
      ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1],
    }[event.key];
    if (!movement) return null;
    const xStep = event.shiftKey ? Math.max(1, Math.round(view.panel.width / view.panel.horizontalDivisions)) : 1;
    const yStep = event.shiftKey ? Math.max(1, Math.round(view.panel.height / view.panel.verticalDivisions)) : 1;
    const current = view.panel.points[view.panel.active] || { x: 0, y: 0 };
    return { x: current.x + movement[0] * xStep, y: current.y + movement[1] * yStep };
  }

  function bindCursorInteraction(view) {
    const applyPointer = event => {
      const point = pointerCursorPoint(event, view.svg, view.panel);
      if (!point) return;
      event.preventDefault?.();
      setCursorPoint(view, point);
    };
    view.svg.addEventListener("pointerdown", applyPointer);
    view.svg.addEventListener("pointermove", event => {
      if (event.buttons === undefined || event.buttons > 0 || event.pointerType === "touch") applyPointer(event);
    });
    view.svg.addEventListener("touchstart", applyPointer, { passive: false });
    view.svg.addEventListener("touchmove", applyPointer, { passive: false });
    view.svg.addEventListener("keydown", event => {
      const point = cursorKeyboardPoint(event, view);
      if (!point) return;
      event.preventDefault?.();
      setCursorPoint(view, point);
    });
  }

  function createCursorView(panel, channels, svg, index) {
    const container = $("#scope-cursor-panels");
    const panelElement = document.createElement("section");
    panelElement.className = "scope-cursor-panel";
    panelElement.setAttribute("role", "group");
    panelElement.setAttribute("aria-label", `Screen cursor panel ${index + 1}`);
    const heading = document.createElement("h3");
    heading.className = "scope-cursor-heading";
    heading.textContent = `${channels.join(" and ")} screen cursor`;
    const controls = document.createElement("div");
    controls.className = "scope-cursor-controls";
    controls.setAttribute("role", "group");
    controls.setAttribute("aria-label", "Active screen cursor");
    const buttons = {};
    for (const name of ["A", "B"]) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "button compact scope-cursor-button";
      button.textContent = `Cursor ${name}`;
      button.setAttribute("aria-label", `Select cursor ${name}`);
      button.setAttribute("aria-pressed", String(panel.active === name));
      button.addEventListener("click", () => setCursorActive(view, name));
      controls.append(button);
      buttons[name] = button;
    }
    const readout = document.createElement("p");
    readout.className = "scope-cursor-readout";
    readout.setAttribute("role", "status");
    readout.setAttribute("aria-live", "polite");
    const status = document.createElement("p");
    status.className = "scope-cursor-status";
    status.setAttribute("role", "status");
    status.setAttribute("aria-live", "polite");
    const view = { panel, channels, svg, buttons, readout, status, markerGroup: null };
    panelElement.append(heading, controls, readout, status);
    container.append(panelElement);
    svg.setAttribute("tabindex", "0");
    svg.setAttribute("aria-describedby", `scope-cursor-readout-${index}`);
    readout.id = `scope-cursor-readout-${index}`;
    bindCursorInteraction(view);
    renderCursorView(view);
    return view;
  }

  function renderScope() {
    const container = $("#scope-plots");
    const cursorContainer = $("#scope-cursor-panels");
    container.replaceChildren();
    cursorContainer.replaceChildren();
    const previousKeys = new Set([...scope.cursorViews.keys(), ...scope.lastCursorKeys]);
    scope.cursorViews = new Map();
    scope.groups = {};
    const channels = Object.keys(scope.slots).filter(channel => scope.slots[channel].capture?.trace);
    const geometry = channel => {
      const trace = scope.slots[channel].capture.trace;
      return cursorIdentity(trace);
    };
    const panels = channels.length === 2 && geometry(channels[0]) !== geometry(channels[1]) ? channels.map(channel => [channel]) : [channels];
    $("#scope-notice").textContent = panels.length === 2 ? "Different screen geometry · channels shown separately" : channels.length ? "Shared screen grid · sequential captures, not simultaneous" : "Awaiting renderable traces · see channel status below";
    for (const [index, panel] of panels.entries()) {
      if (!panel.length) continue;
      const trace = scope.slots[panel[0]].capture.trace;
      const key = cursorIdentity(trace);
      const svg = svgElement("svg", { viewBox: `0 0 ${trace.width} ${trace.height}`, role: "img", "aria-label": `${panel.join(" and ")} vendor screen coordinates; sequential captures, not synchronized`, class: "scope-svg", "data-cursor-panel": key });
      const clip = svgElement("clipPath", { id: `scope-clip-${index}` });
      clip.append(svgElement("rect", { width: trace.width, height: trace.height }));
      const definitions = svgElement("defs"); definitions.append(clip); svg.append(definitions);
      const grid = svgElement("g", { class: "scope-grid" });
      for (let division = 0; division <= trace.horizontalDivisions; division++) {
        const x = division * trace.width / trace.horizontalDivisions;
        grid.append(svgElement("line", { x1: x, x2: x, y1: 0, y2: trace.height }));
      }
      for (let division = 0; division <= trace.verticalDivisions; division++) {
        const y = division * trace.height / trace.verticalDivisions;
        grid.append(svgElement("line", { x1: 0, x2: trace.width, y1: y, y2: y }));
      }
      svg.append(grid);
      for (const channel of panel) {
        const current = scope.slots[channel].capture.trace;
        const group = svgElement("g", { "clip-path": `url(#scope-clip-${index})`, class: channel === "CHANNEL_1" ? "scope-ch1" : "scope-ch2" });
        const path = svgElement("path", { class: "scope-trace", d: current.y.map((y, x) => `${x ? "L" : "M"}${x} ${y}`).join(" "), "stroke-dasharray": channel === "CHANNEL_2" ? "6 3" : "none" });
        const size = Math.min(current.width, current.height) / 40;
        const y = current.groundY;
        const points = channel === "CHANNEL_1" ? `0,${y - size} ${size * 2},${y} 0,${y + size}` : `0,${y} ${size},${y - size} ${size * 2},${y} ${size},${y + size}`;
        group.append(path, svgElement("polygon", { points, class: "scope-ground" }));
        scope.groups[channel] = group; svg.append(group);
      }
      const cursor = cursorPanel(trace, previousKeys);
      const view = createCursorView(cursor, panel, svg, index);
      scope.cursorViews.set(cursor.key, view);
      container.append(svg);
    }
    if (scope.cursorViews.size) scope.lastCursorKeys = new Set(scope.cursorViews.keys());
    renderScopeStatus();
    renderRawSelection();
  }

  function renderScopeStatus() {
    for (const [channel, slot] of Object.entries(scope.slots)) {
      const capture = slot.capture;
      const age = capture ? Math.max(0, performance.now() - slot.received) : 0;
      if (capture && age >= Math.max(3000, scope.interval * 3) && !slot.stale) slot.stale = "No recent capture";
      if (!scope.channels.includes(channel)) slot.stale = "Not subscribed";
      const label = channel === "CHANNEL_1" ? "CH1 solid · triangle ground" : "CH2 dashed · diamond ground";
      const trace = capture?.trace;
      const freshness = slot.stale ? `Stale · ${slot.stale}` : "Automatic captures";
      const statusParts = [label, freshness];
      if (capture) {
        const receiptAge = `received ${(age / 1000).toFixed(1)} s ago`;
        statusParts.push(receiptAge, capture.label);
        if (capture.issue) statusParts.push(capture.issue);
        if (trace) {
          const offScreen = trace.groundY < 0 || trace.groundY > trace.height;
          const ground = `ground ${trace.groundY}${offScreen ? " (off-screen)" : ""}`;
          statusParts.push(trace.profile, ground);
        }
      } else {
        statusParts.push("awaiting capture");
      }
      $(`#scope-${channel === "CHANNEL_1" ? "ch1" : "ch2"}-status`).textContent = statusParts.join(" · ");
      scope.groups[channel]?.setAttribute("opacity", slot.stale ? ".45" : "1");
    }
    for (const view of scope.cursorViews.values()) {
      view.svg.setAttribute("opacity", view.channels.some(channel => scope.slots[channel]?.stale) ? ".65" : "1");
      renderCursorView(view);
    }
  }

  function markScopeStale(reason, channel) {
    for (const [name, slot] of Object.entries(scope.slots)) if (!channel || name === channel) slot.stale = reason;
    renderScopeStatus(); renderRawSelection();
  }

  function startScopeClock() {
    if (scope.timer !== null || state.suspended || document.hidden) return;
    renderScopeStatus();
    renderRawSelection();
    scope.timer = setTimeout(() => { scope.timer = null; startScopeClock(); }, 500);
  }

  function stopScopeClock() {
    clearTimeout(scope.timer); scope.timer = null;
  }

  function updateSequence(sequence) {
    const current = captureSequence(sequence);
    if (current === null) return;
    const previous = state.lastSequence;
    if (previous !== null && current > previous + 1n) {
      state.eventSequenceGaps += Number(current - previous - 1n);
      markScopeStale("Event sequence gap");
    }
    state.lastSequence = current;
    $("#event-sequence").textContent = String(current);
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
    state.streamRevision += 1;
    markScopeStale("Stream reconnecting");
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
    state.streamRevision += 1;
    markScopeStale("Stream stopped");
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
    const waveform = event.waveform;
    const slot = waveform && scope.slots[waveform.channel];
    if (!slot || !scope.channels.includes(waveform.channel)) return;
    const sequence = captureSequence(waveform.sequence);
    if (sequence === null || sequence === 0n) { markScopeStale("Render error: invalid capture sequence", waveform.channel); return; }
    if (slot.sequence !== null && sequence <= slot.sequence) return;
    try {
      slot.capture = admitCapture(waveform, `epoch ${scope.epoch} · capture ${sequence}`);
      slot.sequence = sequence;
      slot.received = performance.now();
      slot.stale = "";
      renderScope();
    } catch (error) { markScopeStale(error.message, waveform.channel); }
    $("#last-update").textContent = `Waveform event ${new Date().toLocaleTimeString()}`;
  }

  function handleGapEvent(source, event) {
    updateSequence(event.sequence);
    const gap = event.waveformGap || {};
    const count = Number(gap.missingCount);
    if (Number.isSafeInteger(count) && count > 0) state.waveformGaps += count;
    $("#waveform-gaps").textContent = String(state.waveformGaps);
    markScopeStale(`Waveform gap: ${gap.missingCount || "unknown"} capture(s) missing`, gap.channel);
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
    state.streamRevision += 1;
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
    scope.interval = interval;
    scope.channels = [];
    const query = new URLSearchParams({ interval: `${interval}ms`, queue_capacity: String(queue), include_controls: String($("#stream-controls").checked), include_screen_header: String($("#stream-header").checked) });
    for (const selector of all("[data-measurement]:checked")) query.append("measurement", selector.dataset.measurement);
    if ($("#stream-wave-ch1").checked) scope.channels.push("CHANNEL_1");
    if ($("#stream-wave-ch2").checked) scope.channels.push("CHANNEL_2");
    for (const channel of scope.channels) query.append("waveform_channel", channel);
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
    markScopeStale("Awaiting stream capture");
    setConnection("Connecting stream · awaiting fresh state", "pending");
    setHydration(state.hasRenderedState ? "Retained state is stale · awaiting fresh state" : "Connecting · awaiting fresh state", "pending");
    const source = new EventSource(state.appliedStreamURL);
    state.source = source;
    source.onopen = () => {
      if (!isCurrentSource(source)) return;
      state.streamRevision += 1;
      scope.epoch += 1;
      for (const slot of Object.values(scope.slots)) slot.sequence = null;
      markScopeStale("Awaiting capture in new connection epoch");
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
    const revision = state.streamRevision;
    setHydration("Loading device identity and complete state…", "pending");
    const results = await Promise.allSettled([requestJSON("/api/device"), requestJSON("/api/state")]);
    // A replacement subscription or page suspension owns all later rendering and startup.
    if (generation !== state.ownerGeneration || revision !== state.streamRevision || state.suspended || state.terminal || state.reconnecting) return;
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
  }

  function setDMMStatus(message, status) {
    const element = $("#dmm-status");
    const holdPrefix = dmm.hostHold ? "Host hold · " : "";
    element.textContent = `${holdPrefix}${message}${dmm.captured ? ` · last captured ${new Date(dmm.captured).toLocaleString()}` : ""}`;
    element.title = dmm.captured;
    element.className = `status-line ${status}`;
  }

  function renderDMMResult(result) {
    $("#metric-dmm").textContent = `${result.value ?? 0} ${dmmUnit(result.unit)}`;
    dmm.captured = result.capturedAt;
  }

  function updateDMMHoldButton() {
    const button = $("#dmm-hold");
    button.textContent = dmm.hostHold ? "Resume live display" : "Hold display";
    button.setAttribute("aria-pressed", String(dmm.hostHold));
    button.setAttribute("aria-label", dmm.hostHold ? "Resume live DMM display" : "Host hold: freeze displayed DMM reading");
  }

  function toggleDMMHold() {
    dmm.hostHold = !dmm.hostHold;
    updateDMMHoldButton();
    if (dmm.hostHold) {
      setDMMStatus(dmm.captured ? "Display frozen locally" : "Waiting for first fresh reading", dmm.stale ? "degraded" : "success");
      return;
    }
    if (dmm.latest) renderDMMResult(dmm.latest);
    if (!dmm.stale) {
      setDMMStatus("Automatic updates", "success");
      return;
    }
    setDMMStatus(`${dmm.captured ? "Stale · " : ""}${dmm.error || "DMM refresh unavailable"}${dmm.active ? " · retrying automatically" : " · use Refresh DMM to retry"}`, "error");
  }

  function isCaptureTimestamp(value) {
    if (typeof value !== "string") return false;
    const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|([+-])(\d{2}):(\d{2}))$/.exec(value);
    if (!parts || parts[0] !== value) return false;
    const [year, month, day, hour, minute, second] = parts.slice(1, 7).map(Number);
    const offsetHour = Number(parts[10] || 0);
    const offsetMinute = Number(parts[11] || 0);
    if (year < 1 || month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 || offsetHour > 23 || offsetMinute > 59) return false;
    const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
    const days = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
    if (day > days[month - 1]) return false;
    // Check the calendar before Date can normalize it; explicit years avoid Date.UTC's 1900 offset for years 1–99.
    const date = new Date(0);
    date.setUTCFullYear(year, month - 1, day);
    const offset = (offsetHour * 60 + offsetMinute) * (parts[9] === "-" ? -1 : 1);
    const milliseconds = Number((parts[7] || "").padEnd(3, "0").slice(0, 3));
    date.setUTCHours(hour, minute - offset, second, milliseconds);
    return date.getUTCFullYear() >= 1 && date.getUTCFullYear() <= 9999;
  }

  function pauseDMM(reason) {
    dmm.active = false;
    dmm.stale = true;
    dmm.generation += 1;
    clearTimeout(dmm.timer);
    dmm.timer = null;
    dmm.controller?.abort();
    setDMMStatus(`${dmm.captured ? "Stale · " : ""}${reason}`, "degraded");
  }

  function resumeDMM() {
    if (dmm.active || dmm.configuring || state.suspended || document.hidden) return;
    dmm.active = true;
    refreshDMM();
  }

  async function refreshDMM() {
    if (dmm.controller || dmm.configuring || state.suspended || document.hidden) return;
    clearTimeout(dmm.timer);
    dmm.timer = null;
    const generation = dmm.generation;
    const controller = new AbortController();
    dmm.controller = controller;
    const button = $("[data-get]");
    buttonBusy(button, true);
    setDMMStatus(`${dmm.stale && dmm.captured ? "Stale · " : ""}Reading DMM…`, "pending");
    try {
      const result = await requestJSON("/api/dmm/measurement", { signal: controller.signal });
      if (generation !== dmm.generation) return;
      if (!result || typeof result !== "object" || Array.isArray(result)
        || typeof result.raw !== "string" || !result.raw.trim()
        || !isCaptureTimestamp(result.capturedAt)
        || (Object.hasOwn(result, "value") && (typeof result.value !== "number" || !Number.isFinite(result.value)))) {
        throw new Error("DMM protocol error: measurement requires raw reading, valid capture time and finite numeric value");
      }
      dmm.latest = result;
      if (!dmm.hostHold || !dmm.captured) renderDMMResult(result);
      dmm.stale = false;
      dmm.error = "";
      setDMMStatus(dmm.hostHold ? "Fresh reading retained" : "Automatic updates", "success");
    } catch (error) {
      if (generation !== dmm.generation) return;
      dmm.stale = true;
      dmm.error = error.message;
      setDMMStatus(`${dmm.captured ? "Stale · " : ""}${error.message}${dmm.active ? " · retrying automatically" : " · use Refresh DMM to retry"}`, "error");
    } finally {
      if (dmm.controller === controller) {
        dmm.controller = null;
        buttonBusy(button, false);
        if (dmm.active) {
          if (generation !== dmm.generation) refreshDMM();
          else dmm.timer = setTimeout(refreshDMM, 1000);
        }
      }
    }
  }

  async function action(button) {
    buttonBusy(button, true);
    try {
      const result = await requestJSON(button.dataset.action, { method: "POST", body: "" });
      const message = button.dataset.resultSemantics === "no-response-unverified"
        ? "Auto command sent; device response/readback unavailable; effect unverified."
        : appliedMessage(result, `${button.dataset.action.replace("/api/", "")} applied`);
      showToast(message);
    } catch (error) {
      showToast(error.message, true);
    } finally {
      buttonBusy(button, false);
    }
  }

  document.addEventListener("change", markTouched);
  document.addEventListener("input", markTouched);
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) { pauseDMM("Page hidden · updates paused"); stopScopeClock(); }
    else { resumeDMM(); startScopeClock(); }
  });
  updateDMMApplicability($('form[data-api="/api/dmm"]'));
  all("form[data-api]").forEach((form) => {
    const feedback = form.querySelector("[data-form-status]");
    form.setAttribute("aria-describedby", feedback.id);
    if (form.dataset.api === "/api/dmm") form.elements.currentType.setAttribute("aria-describedby", "dmm-current-note dmm-current-error");
    if (form.dataset.api === "/api/generator") {
      for (const input of form.elements) if (input.name) input.setAttribute("aria-describedby", `generator-${input.name}-pending generator-${input.name}-error`);
      renderGeneratorDrafts(form);
    }
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      submitForm(form);
    });
  });
  all("[data-action]").forEach((button) => button.addEventListener("click", () => action(button)));
  all("[data-get]").forEach((button) => button.addEventListener("click", refreshDMM));
  $("#dmm-hold").addEventListener("click", toggleDMMHold);
  updateDMMHoldButton();
  $("#stream-apply").addEventListener("click", () => {
    state.appliedStreamURL = normalizeStreamOptionsAndBuildURL();
    state.terminal = false;
    connectEvents();
    showToast("Subscribe options applied");
  });
  $("#download-command").addEventListener("click", () => downloadBase64(state.command, "owon-response.bin"));
  $("#raw-source").addEventListener("change", renderRawSelection);
  $("#download-waveform").addEventListener("click", () => {
    const capture = selectedCapture()?.capture;
    if (capture?.data) downloadBase64(capture.data, `${capture.channel}-${capture.identity}-${capture.end}.bin`.replace(/[^\w.-]/g, "_"));
  });
  $("#download-trace-csv").addEventListener("click", () => {
    const capture = selectedCapture()?.capture;
    if (capture?.trace && !capture.issue) downloadText(traceCSV(capture), `${capture.channel}-${capture.identity}-${capture.end}.csv`.replace(/[^\w.-]/g, "_"), "text/csv;charset=utf-8");
  });
  $("#download-trace-svg").addEventListener("click", () => {
    const capture = selectedCapture()?.capture;
    if (capture?.trace && !capture.issue) downloadText(traceSVG(capture), `${capture.channel}-${capture.identity}-${capture.end}.svg`.replace(/[^\w.-]/g, "_"), "image/svg+xml;charset=utf-8");
  });
  $("#inspect-waveform").addEventListener("click", () => {
    const capture = selectedCapture()?.capture;
    if (capture) $("#waveform-header").textContent = `${capture.label}\nFrozen raw inspection · base64:\n${capture.data}`;
  });
  $("#inspect-header").addEventListener("click", () => {
    const capture = selectedCapture()?.capture;
    if (capture) $("#waveform-header").textContent = `${capture.label}\nFrozen header inspection:\n${renderHeader(capture.header)}`;
  });
  window.addEventListener("pagehide", () => {
    pauseDMM("Page suspended · updates paused");
    stopScopeClock();
    markScopeStale("Page suspended");
    scope.manual.generation += 1;
    if (scope.manual.controller) setFormStatus(all("form[data-api]").find(form => form.dataset.api === "/api/waveform"), "Capture cancelled by page suspension", "error");
    scope.manual.controller?.abort();
    scope.manual.stale = "Page suspended";
    renderRawSelection();
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
    resumeDMM();
    startScopeClock();
    if (state.terminal) {
      setHydration(state.hasRenderedState ? "Retained state is stale · stream stopped" : "Device stream stopped · apply to retry", "error");
      return;
    }
    connectEvents();
  });
  state.appliedStreamURL = normalizeStreamOptionsAndBuildURL();
  resumeDMM();
  connectEvents();
  startScopeClock();
  hydrate();
})();
