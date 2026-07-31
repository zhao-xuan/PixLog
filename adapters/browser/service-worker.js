async function settings() {
  const stored = await chrome.storage.local.get(["daemonURL", "captureToken"]);
  return {
    daemonURL: (stored.daemonURL || "http://127.0.0.1:4777").replace(/\/$/, ""),
    captureToken: stored.captureToken || ""
  };
}

async function pixlogRequest(path, body) {
  const configured = await settings();
  const response = await fetch(`${configured.daemonURL}${path}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-PixLog-Token": configured.captureToken
    },
    body: JSON.stringify(body)
  });
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(payload.error || `PixLog daemon returned ${response.status}`);
  }
  return payload;
}

async function captureObservation(observation) {
  const parsedURL = new URL(observation.url);
  const session = await pixlogRequest("/v1/sessions", {
    adapter: "browser-extension",
    adapter_version: "0.1.0",
    application: parsedURL.hostname,
    document_id: observation.url,
    metadata: { title: observation.title }
  });
  const event = await pixlogRequest("/v1/events", {
    session_id: session.id,
    event_type: "browser.generation-observed",
    fidelity: "ui-observed",
    occurred_at: observation.observed_at,
    raw_payload: observation,
    normalized: {
      schema: "pixlog.operation/v1",
      operation: "ai.generation",
      provider: parsedURL.hostname,
      prompts: observation.prompts,
      options: observation.options,
      outputs: observation.images
    }
  });
  await pixlogRequest(`/v1/sessions/${session.id}/finish`, {});
  await chrome.storage.session.set({ latestSessionId: session.id });
  return event;
}

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message.type === "capture-page") {
    captureObservation(message.observation)
      .then((event) => sendResponse({ ok: true, eventId: event.id }))
      .catch((error) => sendResponse({ ok: false, error: error.message }));
    return true;
  }
  if (message.type === "capture-download") {
    Promise.all([
      chrome.downloads.search({ orderBy: ["-startTime"], limit: 1 }),
      chrome.storage.session.get("latestSessionId")
    ]).then(async ([downloads, state]) => {
      if (!downloads[0] || !state.latestSessionId) {
        throw new Error("Capture a page and complete a download first");
      }
      const download = downloads[0];
      await pixlogRequest("/v1/events", {
        session_id: state.latestSessionId,
        event_type: "browser.download-observed",
        fidelity: "ui-observed",
        raw_payload: {
          url: download.url,
          final_url: download.finalUrl,
          filename: download.filename,
          mime: download.mime,
          file_size: download.fileSize
        },
        normalized: {
          schema: "pixlog.operation/v1",
          operation: "artifact.download",
          filename: download.filename,
          media_type: download.mime,
          size: download.fileSize
        }
      });
      sendResponse({ ok: true, filename: download.filename });
    }).catch((error) => sendResponse({ ok: false, error: error.message }));
    return true;
  }
  return false;
});