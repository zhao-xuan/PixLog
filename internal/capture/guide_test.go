package capture

import (
	"strings"
	"testing"
)

func TestProxyPlatformGuidesUseSessionWorkflow(t *testing.T) {
	for _, platform := range []string{"comfyui", "automatic1111", "openai", "firefly"} {
		guide, err := PlatformGuide(platform)
		if err != nil {
			t.Fatalf("PlatformGuide(%q): %v", platform, err)
		}
		if guide.VerificationCommand != "pixlog capture sessions" {
			t.Errorf("%s verification = %q", platform, guide.VerificationCommand)
		}
		if !strings.Contains(strings.Join(guide.Steps, "\n"), "capture finalize") {
			t.Errorf("%s guide does not explain session finalization", platform)
		}
	}
}
