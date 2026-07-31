const daemonURL = document.getElementById("daemon-url");
const captureToken = document.getElementById("capture-token");
const statusElement = document.getElementById("status");

chrome.storage.local.get(["daemonURL", "captureToken"], (settings) => {
  daemonURL.value = settings.daemonURL || daemonURL.value;
  captureToken.value = settings.captureToken || "";
});

document.getElementById("save").addEventListener("click", async () => {
  const settings = { daemonURL: daemonURL.value.replace(/\/$/, ""), captureToken: captureToken.value };
  await chrome.storage.local.set(settings);
  try {
    const response = await fetch(`${settings.daemonURL}/v1/health`, {
      headers: { "X-PixLog-Token": settings.captureToken }
    });
    if (!response.ok) {
      throw new Error(`daemon returned ${response.status}`);
    }
    statusElement.textContent = "Connected";
  } catch (error) {
    statusElement.textContent = error.message;
  }
});