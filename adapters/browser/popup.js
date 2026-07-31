const statusElement = document.getElementById("status");

function setStatus(message, isError = false) {
  statusElement.textContent = message;
  statusElement.style.color = isError ? "#a40000" : "";
}

document.getElementById("capture-page").addEventListener("click", async () => {
  try {
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    const [{ result }] = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => {
        const visible = (element) => Boolean(element.offsetWidth || element.offsetHeight || element.getClientRects().length);
        const prompts = [...document.querySelectorAll("textarea, input[type='text']")]
          .filter(visible)
          .map((element) => ({ label: element.getAttribute("aria-label") || element.name || element.placeholder || "", value: element.value }))
          .filter((entry) => entry.value);
        const options = [...document.querySelectorAll("select")]
          .filter(visible)
          .map((element) => ({ label: element.getAttribute("aria-label") || element.name || "", value: element.value }));
        const images = [...document.images]
          .filter(visible)
          .slice(0, 20)
          .map((image) => ({ src: image.currentSrc || image.src, alt: image.alt, width: image.naturalWidth, height: image.naturalHeight }));
        return {
          url: location.href,
          title: document.title,
          prompts,
          options,
          images,
          observed_at: new Date().toISOString()
        };
      }
    });
    const response = await chrome.runtime.sendMessage({ type: "capture-page", observation: result });
    if (!response?.ok) {
      throw new Error(response?.error || "Capture failed");
    }
    setStatus(`Captured ${response.eventId}`);
  } catch (error) {
    setStatus(error.message, true);
  }
});

document.getElementById("capture-download").addEventListener("click", async () => {
  const response = await chrome.runtime.sendMessage({ type: "capture-download" });
  if (!response?.ok) {
    setStatus(response?.error || "No download available", true);
    return;
  }
  setStatus(`Attached ${response.filename}`);
});

document.getElementById("open-options").addEventListener("click", () => chrome.runtime.openOptionsPage());