const photoshop = require("photoshop");

const EVENT_ALLOW_LIST = [
  "open",
  "close",
  "save",
  "saveAs",
  "export",
  "crop",
  "curves",
  "imageSize",
  "canvasSize",
  "make",
  "delete",
  "set",
  "transform",
  "placedLayerReplaceContents",
  "mergeLayersNew",
  "flattenImage",
  "generativeFill",
  "paint"
];

const OPERATION_TYPES = {
  crop: "geometry.crop",
  curves: "color.curves",
  imageSize: "geometry.resize",
  canvasSize: "geometry.canvas-size",
  transform: "geometry.transform",
  placedLayerReplaceContents: "layer.smart-object.replace",
  mergeLayersNew: "layer.merge",
  flattenImage: "document.flatten",
  generativeFill: "ai.inpaint",
  paint: "paint.stroke"
};

const CHECKPOINT_EVENTS = new Set([
  "save",
  "saveAs",
  "export",
  "crop",
  "imageSize",
  "canvasSize",
  "placedLayerReplaceContents",
  "mergeLayersNew",
  "flattenImage",
  "generativeFill"
]);

let sessionId = "";
let registeredEvents = [];
let paintEvents = [];
let paintTimer;

const daemonURL = document.getElementById("daemon-url");
const captureToken = document.getElementById("capture-token");
const startButton = document.getElementById("start");
const stopButton = document.getElementById("stop");
const checkpointButton = document.getElementById("checkpoint");
const statusElement = document.getElementById("status");

daemonURL.value = localStorage.getItem("pixlog.daemonURL") || daemonURL.value;

function setStatus(message, isError = false) {
  statusElement.textContent = message;
  statusElement.style.color = isError ? "#e35b5b" : "";
}

async function pixlogRequest(path, body) {
  const response = await fetch(`${daemonURL.value.replace(/\/$/, "")}${path}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-PixLog-Token": captureToken.value
    },
    body: JSON.stringify(body)
  });
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(payload.error || `PixLog daemon returned ${response.status}`);
  }
  return payload;
}

function documentState() {
  const active = photoshop.app.activeDocument;
  if (!active) {
    return {};
  }
  return {
    document_id: String(active.id),
    title: active.title,
    width: Number(active.width),
    height: Number(active.height),
    resolution: Number(active.resolution),
    active_layer_id: active.activeLayers?.[0]?.id || null,
    active_layer_name: active.activeLayers?.[0]?.name || null,
    photoshop_version: photoshop.app.version
  };
}

function normalizeEvent(event, descriptor) {
  const state = documentState();
  const normalized = {
    schema: "pixlog.operation/v1",
    operation: OPERATION_TYPES[event] || `adobe.photoshop.${event}`,
    document: state,
    target: {
      layer_id: state.active_layer_id,
      layer_name: state.active_layer_name
    }
  };
  if (event === "generativeFill") {
    const exposedPrompt = descriptor?.prompt || descriptor?.textPrompt || null;
    normalized.parameters = { prompt: exposedPrompt };
    normalized.unknown_fields = ["seed", "exact_model_revision", "server_side_prompt_processing"];
  }
  return normalized;
}

async function sendEvent(event, descriptor) {
  if (!sessionId) {
    return;
  }
  if (event === "paint") {
    paintEvents.push({ occurred_at: new Date().toISOString(), descriptor });
    clearTimeout(paintTimer);
    paintTimer = setTimeout(flushPaintEvents, 500);
    return;
  }
  await pixlogRequest("/v1/events", {
    session_id: sessionId,
    event_type: event,
    fidelity: "exact-command",
    occurred_at: new Date().toISOString(),
    raw_payload: descriptor,
    normalized: normalizeEvent(event, descriptor)
  });
  if (CHECKPOINT_EVENTS.has(event)) {
    await createCheckpoint(event);
  }
}

async function flushPaintEvents() {
  if (!sessionId || paintEvents.length === 0) {
    return;
  }
  const batch = paintEvents.splice(0, paintEvents.length);
  await pixlogRequest("/v1/events", {
    session_id: sessionId,
    event_type: "paint.stroke_group",
    fidelity: "exact-command",
    occurred_at: batch[0].occurred_at,
    raw_payload: { count: batch.length, events: batch },
    normalized: {
      schema: "pixlog.operation/v1",
      operation: "paint.stroke-group",
      count: batch.length,
      document: documentState()
    }
  });
}

async function createCheckpoint(reason = "user-requested") {
  if (!sessionId) {
    return;
  }
  const state = documentState();
  await pixlogRequest("/v1/checkpoints", {
    session_id: sessionId,
    document_id: state.document_id || "",
    reason,
    metadata: state
  });
}

async function actionListener(event, descriptor) {
  try {
    await sendEvent(event, descriptor);
    setStatus(`Capturing ${event}`);
  } catch (error) {
    setStatus(error.message, true);
  }
}

async function startCapture() {
  try {
    localStorage.setItem("pixlog.daemonURL", daemonURL.value);
    const state = documentState();
    const session = await pixlogRequest("/v1/sessions", {
      adapter: "photoshop-uxp",
      adapter_version: "0.1.0",
      application: "Adobe Photoshop",
      document_id: state.document_id || "",
      metadata: state
    });
    sessionId = session.id;
    registeredEvents = [];
    for (const event of EVENT_ALLOW_LIST) {
      try {
        await photoshop.action.addNotificationListener([event], actionListener);
        registeredEvents.push(event);
      } catch (_) {
        // Event availability varies by Photoshop version; unsupported events stay disabled.
      }
    }
    startButton.disabled = true;
    stopButton.disabled = false;
    checkpointButton.disabled = false;
    setStatus(`Capturing session ${sessionId.slice(0, 20)}...`);
  } catch (error) {
    setStatus(error.message, true);
  }
}

async function stopCapture() {
  try {
    clearTimeout(paintTimer);
    await flushPaintEvents();
    if (registeredEvents.length > 0) {
      photoshop.action.removeNotificationListener(registeredEvents, actionListener);
    }
    if (sessionId) {
      await pixlogRequest(`/v1/sessions/${sessionId}/finish`, {});
    }
  } catch (error) {
    setStatus(error.message, true);
  } finally {
    sessionId = "";
    registeredEvents = [];
    startButton.disabled = false;
    stopButton.disabled = true;
    checkpointButton.disabled = true;
    if (!statusElement.style.color) {
      setStatus("Not capturing");
    }
  }
}

startButton.addEventListener("click", startCapture);
stopButton.addEventListener("click", stopCapture);
checkpointButton.addEventListener("click", () => createCheckpoint());