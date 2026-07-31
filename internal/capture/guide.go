package capture

import (
	"fmt"
	"sort"
	"strings"
)

type Guide struct {
	Platform            string   `json:"platform"`
	Adapter             string   `json:"adapter"`
	CaptureFidelity     string   `json:"capture_fidelity"`
	Reproducibility     string   `json:"reproducibility"`
	Summary             string   `json:"summary"`
	Steps               []string `json:"steps"`
	VerificationCommand string   `json:"verification_command"`
}

var platformGuides = map[string]Guide{
	"photoshop": {
		Platform:        "photoshop",
		Adapter:         "photoshop-uxp",
		CaptureFidelity: "exact-command or application-history",
		Reproducibility: "best-effort or provenance-only",
		Summary:         "Capture allow-listed Photoshop actions and raw Action Descriptors through the PixLog UXP plugin.",
		Steps: []string{
			"Set PIXLOG_CAPTURE_TOKEN to a random value from `pixlog capture token`.",
			"Start `pixlog capture serve` in the Git worktree.",
			"Load adapters/photoshop in Photoshop UXP Developer Tool.",
			"Set the daemon URL and token in the PixLog plugin panel, then open a document.",
			"After saving, run `pixlog capture sessions`, then `pixlog capture finalize <session-id> <asset>`.",
			"Fallback: enable Photoshop History Log with detailed items, then run `pixlog capture history <history-log> <asset>`.",
		},
		VerificationCommand: "pixlog capture status",
	},
	"comfyui": {
		Platform:        "comfyui",
		Adapter:         "comfyui-proxy",
		CaptureFidelity: "exact-request",
		Reproducibility: "best-effort",
		Summary:         "Capture workflow submissions before execution and map prompt IDs to provider job records.",
		Steps: []string{
			"Start ComfyUI on its normal loopback address, usually 127.0.0.1:8188.",
			"Start `pixlog capture proxy --platform comfyui --upstream http://127.0.0.1:8188 --listen 127.0.0.1:8189`.",
			"Point the ComfyUI client at http://127.0.0.1:8189.",
			"Keep workflow model paths stable; PixLog records the request and prompt job mapping but cannot make nondeterministic kernels exact.",
			"Run `pixlog capture sessions`, then `pixlog capture finalize <session-id> <output-asset>`.",
		},
		VerificationCommand: "pixlog capture sessions",
	},
	"automatic1111": {
		Platform:        "automatic1111",
		Adapter:         "automatic1111-proxy",
		CaptureFidelity: "exact-request",
		Reproducibility: "best-effort",
		Summary:         "Capture txt2img/img2img API requests and responses, including extension-owned JSON fields.",
		Steps: []string{
			"Start AUTOMATIC1111 or Forge with its API enabled on 127.0.0.1:7860.",
			"Start `pixlog capture proxy --platform automatic1111 --upstream http://127.0.0.1:7860 --listen 127.0.0.1:7861`.",
			"Send API requests to http://127.0.0.1:7861 instead of port 7860.",
			"For previously generated PNG files, ordinary `git add` still imports embedded parameters as a fallback.",
			"Run `pixlog capture sessions`, then `pixlog capture finalize <session-id> <output-asset>`.",
		},
		VerificationCommand: "pixlog capture sessions",
	},
	"openai": {
		Platform:        "openai",
		Adapter:         "openai-proxy",
		CaptureFidelity: "exact-request",
		Reproducibility: "request-reproducible",
		Summary:         "Capture image API request/response envelopes while removing credentials and signed URLs.",
		Steps: []string{
			"Start `pixlog capture proxy --platform openai --upstream https://api.openai.com --listen 127.0.0.1:4780`.",
			"Configure the SDK base URL as http://127.0.0.1:4780/v1.",
			"Keep the provider API key in the SDK environment; PixLog forwards but never stores authorization headers.",
			"Run `pixlog capture sessions`, then `pixlog capture finalize <session-id> <output-asset>`.",
			"Replay is available only when the selected endpoint accepts the optional Bearer token injected by `--auth-env`.",
		},
		VerificationCommand: "pixlog capture sessions",
	},
	"firefly": {
		Platform:        "firefly",
		Adapter:         "firefly-proxy",
		CaptureFidelity: "exact-request",
		Reproducibility: "request-reproducible",
		Summary:         "Capture Firefly asynchronous requests, job IDs, status responses, references, and output URLs.",
		Steps: []string{
			"Start `pixlog capture proxy --platform firefly --upstream <Firefly API origin> --listen 127.0.0.1:4781`.",
			"Configure the Firefly client base URL as http://127.0.0.1:4781.",
			"Keep OAuth credentials outside recipes; PixLog redacts headers and signed result URLs.",
			"Run `pixlog capture sessions`, then `pixlog capture finalize <session-id> <output-asset>`.",
			"Replay is available only when the selected endpoint accepts the optional Bearer token injected by `--auth-env`.",
		},
		VerificationCommand: "pixlog capture sessions",
	},
	"browser": {
		Platform:        "browser",
		Adapter:         "browser-extension",
		CaptureFidelity: "ui-observed",
		Reproducibility: "provenance-only",
		Summary:         "Capture user-observed prompts, options, job IDs, references, and downloads from tools without an API.",
		Steps: []string{
			"Set PIXLOG_CAPTURE_TOKEN and start `pixlog capture serve`.",
			"Load adapters/browser as an unpacked Chromium extension.",
			"Configure the daemon URL and token from the extension options page.",
			"Use the PixLog extension action only on sites whose terms and privacy policy permit capture.",
			"After downloading an output, run `pixlog capture sessions`, then `pixlog capture finalize <session-id> <asset>`.",
		},
		VerificationCommand: "pixlog capture status",
	},
	"metadata": {
		Platform:        "metadata",
		Adapter:         "embedded-metadata",
		CaptureFidelity: "embedded-metadata",
		Reproducibility: "best-effort or provenance-only",
		Summary:         "Import PNG text, EXIF, XMP, ICC, and C2PA metadata when native capture is unavailable.",
		Steps: []string{
			"Run `pixlog metadata inspect <asset>` to review embedded metadata.",
			"Run `pixlog metadata import <asset>` to create and associate a provenance recipe.",
			"Install c2patool and run `pixlog c2pa verify <asset>` for signed Content Credentials.",
		},
		VerificationCommand: "pixlog recipe show <asset>",
	},
}

func PlatformGuide(platform string) (Guide, error) {
	platform = normalizePlatform(platform)
	guide, exists := platformGuides[platform]
	if !exists {
		return Guide{}, fmt.Errorf("unknown capture platform %q; available: %s", platform, strings.Join(Platforms(), ", "))
	}
	return guide, nil
}

func Platforms() []string {
	platforms := make([]string, 0, len(platformGuides))
	for platform := range platformGuides {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	return platforms
}

func normalizePlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "adobe", "photoshop-uxp", "ps":
		return "photoshop"
	case "a1111", "forge", "stable-diffusion-webui":
		return "automatic1111"
	case "comfy":
		return "comfyui"
	case "c2pa", "exif", "xmp":
		return "metadata"
	default:
		return strings.ToLower(strings.TrimSpace(platform))
	}
}
