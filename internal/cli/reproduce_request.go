package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pixlog/pixlog/internal/capture"
	"github.com/pixlog/pixlog/internal/recipe"
	"github.com/pixlog/pixlog/internal/repository"
)

const maxReproductionResponse = 32 << 20

func executeCapturedRequest(repo *repository.GitRepository, plan repository.ReproductionPlan, baseURL, authEnv, responseOutput string, timeout time.Duration, stdout io.Writer) error {
	target, err := repo.ValidateRequestReproduction(plan, baseURL)
	if err != nil {
		return err
	}
	payload, err := repo.ReproductionRequestData(plan)
	if err != nil {
		return err
	}
	if incompleteReproductionPayload(payload) {
		return errors.New("captured request contains redacted fields and cannot be replayed without an edited recipe")
	}
	token := ""
	if authEnv != "" {
		token = os.Getenv(authEnv)
		if token == "" {
			return fmt.Errorf("environment variable %s is empty", authEnv)
		}
	}
	request, err := http.NewRequest(strings.ToUpper(plan.Request.Method), target.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	contentType := plan.Request.ContentType
	if contentType == "" && json.Valid(payload) {
		contentType = "application/json"
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if timeout <= 0 {
		return errors.New("reproduction timeout must be positive")
	}
	startedAt := time.Now().UTC()
	response, err := (&http.Client{Timeout: timeout}).Do(request)
	if err != nil {
		return fmt.Errorf("replay provider request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxReproductionResponse+1))
	if err != nil {
		return fmt.Errorf("read provider response: %w", err)
	}
	if len(body) > maxReproductionResponse {
		return fmt.Errorf("provider response exceeds %d bytes", maxReproductionResponse)
	}
	store, err := repository.OpenGitMediaStore(repo.Root)
	if err != nil {
		return err
	}
	redactedResponse, err := capture.RedactPayload(response.Header.Get("Content-Type"), body)
	if err != nil {
		return err
	}
	responseOID, err := store.Put(redactedResponse)
	if err != nil {
		return err
	}
	journal, err := repository.OpenProvenanceJournal(repo.Root)
	if err != nil {
		return err
	}
	defer journal.Close()
	session, err := journal.StartCaptureSession(repository.CaptureSession{
		Adapter:        plan.Request.Adapter + "-replay",
		AdapterVersion: "1",
		Application:    plan.Request.Provider,
		Metadata: mustCLIJSON(map[string]any{
			"source_recipe_oid": plan.RecipeOID,
			"target_origin":     target.Scheme + "://" + target.Host,
		}),
	})
	if err != nil {
		return err
	}
	event, err := journal.AppendCaptureEvent(repository.CaptureEvent{
		SessionID:     session.ID,
		EventType:     plan.Request.Provider + ".request-replayed",
		OccurredAt:    startedAt,
		Fidelity:      recipe.FidelityExactRequest,
		RawPayloadOID: plan.Request.RequestOID,
		Normalized: mustCLIJSON(map[string]any{
			"provider":      plan.Request.Provider,
			"method":        plan.Request.Method,
			"path":          plan.Request.Path,
			"status_code":   response.StatusCode,
			"response_oid":  responseOID,
			"duration_ms":   time.Since(startedAt).Milliseconds(),
			"source_recipe": plan.RecipeOID,
		}),
	})
	if err != nil {
		return err
	}
	outputOIDs := []string{}
	if strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "image/") {
		outputOIDs = append(outputOIDs, responseOID)
	}
	for _, imageData := range capture.ProviderResponseImages(plan.Request.Provider, body) {
		oid, err := store.Put(imageData)
		if err != nil {
			return err
		}
		outputOIDs = append(outputOIDs, oid)
	}
	recordedOutputs := map[string]bool{}
	for _, oid := range outputOIDs {
		if recordedOutputs[oid] {
			continue
		}
		recordedOutputs[oid] = true
		if _, err := journal.RecordCaptureArtifact(repository.CaptureArtifact{
			SessionID: session.ID, EventID: event.ID, Role: "output", ContentOID: oid,
		}); err != nil {
			return err
		}
	}
	if err := journal.FinishCaptureSession(session.ID, time.Time{}); err != nil {
		return err
	}
	if responseOutput != "" {
		if err := os.WriteFile(responseOutput, body, 0o600); err != nil {
			return fmt.Errorf("write provider response: %w", err)
		}
	}
	fmt.Fprintf(stdout, "Replayed %s request: HTTP %d, response %s, session %s\n", plan.Request.Provider, response.StatusCode, repository.ShortOID(responseOID), session.ID)
	if responseOutput == "" && (json.Valid(body) || strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/")) {
		if _, err := stdout.Write(body); err != nil {
			return err
		}
		if len(body) == 0 || body[len(body)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("provider returned HTTP %d", response.StatusCode)
	}
	return nil
}

func incompleteReproductionPayload(payload []byte) bool {
	if bytes.Contains(payload, []byte("[REDACTED]")) || bytes.Contains(payload, []byte(`"capture":"digest-only-unstructured-payload"`)) {
		return true
	}
	decoded, err := url.QueryUnescape(string(payload))
	return err == nil && strings.Contains(decoded, "[REDACTED]")
}

func mustCLIJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
