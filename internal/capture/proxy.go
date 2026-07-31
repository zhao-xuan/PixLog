package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pixlog/pixlog/internal/recipe"
	"github.com/pixlog/pixlog/internal/repository"
)

type ProxyOptions struct {
	Platform string
	Upstream string
}

type Proxy struct {
	platform  string
	upstream  *url.URL
	store     *repository.GitMediaStore
	journal   *repository.ProvenanceJournal
	session   repository.CaptureSession
	handler   http.Handler
	closeOnce sync.Once
	closeErr  error
}

type proxyCapture struct {
	requestOID  string
	method      string
	path        string
	startedAt   time.Time
	contentType string
}

type proxyCaptureKey struct{}

func NewProxy(start string, options ProxyOptions) (*Proxy, error) {
	platform := normalizePlatform(options.Platform)
	if _, err := PlatformGuide(platform); err != nil && platform != "generic" {
		return nil, err
	}
	upstream, err := url.Parse(options.Upstream)
	if err != nil || upstream.Host == "" || upstream.User != nil || upstream.Scheme != "http" && upstream.Scheme != "https" {
		return nil, fmt.Errorf("invalid proxy upstream %q", options.Upstream)
	}
	store, err := repository.OpenGitMediaStore(start)
	if err != nil {
		return nil, err
	}
	journal, err := repository.OpenProvenanceJournal(start)
	if err != nil {
		return nil, err
	}
	session, err := journal.StartCaptureSession(repository.CaptureSession{
		Adapter:        platform + "-proxy",
		AdapterVersion: "1",
		Application:    platform,
		Metadata:       mustProxyJSON(map[string]any{"upstream": upstream.Scheme + "://" + upstream.Host}),
	})
	if err != nil {
		journal.Close()
		return nil, err
	}
	proxy := &Proxy{platform: platform, upstream: upstream, store: store, journal: journal, session: session}
	reverseProxy := httputil.NewSingleHostReverseProxy(upstream)
	originalDirector := reverseProxy.Director
	reverseProxy.Director = func(request *http.Request) {
		originalHost := request.Host
		originalDirector(request)
		request.Header.Set("X-Forwarded-Host", originalHost)
		request.Host = upstream.Host
	}
	reverseProxy.ModifyResponse = proxy.modifyResponse
	reverseProxy.ErrorHandler = proxy.handleProxyError
	proxy.handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/__pixlog/health" {
			writeAPIJSON(writer, http.StatusOK, map[string]any{
				"schema":     APISchema,
				"status":     "ok",
				"platform":   proxy.platform,
				"session_id": proxy.session.ID,
			})
			return
		}
		captured, err := proxy.captureRequest(request)
		if err != nil {
			writeAPIError(writer, http.StatusBadRequest, err)
			return
		}
		context := context.WithValue(request.Context(), proxyCaptureKey{}, captured)
		reverseProxy.ServeHTTP(writer, request.WithContext(context))
	})
	return proxy, nil
}

func (proxy *Proxy) Handler() http.Handler {
	return proxy.handler
}

func (proxy *Proxy) SessionID() string {
	return proxy.session.ID
}

func (proxy *Proxy) Close() error {
	proxy.closeOnce.Do(func() {
		if err := proxy.journal.FinishCaptureSession(proxy.session.ID, time.Time{}); err != nil {
			proxy.closeErr = err
		}
		if err := proxy.journal.Close(); err != nil && proxy.closeErr == nil {
			proxy.closeErr = err
		}
	})
	return proxy.closeErr
}

func (proxy *Proxy) captureRequest(request *http.Request) (*proxyCapture, error) {
	captured := &proxyCapture{
		method:      request.Method,
		path:        RedactURL(request.URL.RequestURI()),
		startedAt:   time.Now().UTC(),
		contentType: request.Header.Get("Content-Type"),
	}
	if request.Body == nil {
		return captured, nil
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxCaptureBody+1))
	if err != nil {
		return nil, fmt.Errorf("read proxied request: %w", err)
	}
	if len(body) > maxCaptureBody {
		return nil, fmt.Errorf("proxied request exceeds %d bytes", maxCaptureBody)
	}
	request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	if len(body) == 0 {
		return captured, nil
	}
	redacted, err := RedactPayload(captured.contentType, body)
	if err != nil {
		return nil, err
	}
	captured.requestOID, err = proxy.store.Put(redacted)
	if err != nil {
		return nil, err
	}
	return captured, nil
}

func (proxy *Proxy) modifyResponse(response *http.Response) error {
	captured, _ := response.Request.Context().Value(proxyCaptureKey{}).(*proxyCapture)
	if captured == nil {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCaptureBody+1))
	if err != nil {
		return fmt.Errorf("read proxied response: %w", err)
	}
	if len(body) > maxCaptureBody {
		return fmt.Errorf("proxied response exceeds %d bytes", maxCaptureBody)
	}
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	responseOID := ""
	if len(body) > 0 {
		redacted, redactErr := RedactPayload(response.Header.Get("Content-Type"), body)
		if redactErr != nil {
			return redactErr
		}
		responseOID, err = proxy.store.Put(redacted)
		if err != nil {
			return err
		}
	}
	normalized := map[string]any{
		"platform":     proxy.platform,
		"method":       captured.method,
		"path":         captured.path,
		"status_code":  response.StatusCode,
		"request_oid":  captured.requestOID,
		"response_oid": responseOID,
		"duration_ms":  time.Since(captured.startedAt).Milliseconds(),
	}
	event, err := proxy.journal.AppendCaptureEvent(repository.CaptureEvent{
		SessionID:     proxy.session.ID,
		EventType:     proxyEventType(proxy.platform, captured.method, captured.path),
		OccurredAt:    captured.startedAt,
		Fidelity:      recipe.FidelityExactRequest,
		RawPayloadOID: captured.requestOID,
		Normalized:    mustProxyJSON(normalized),
	})
	if err != nil {
		return err
	}
	if externalID, status := proxyJob(body, proxy.platform, response.StatusCode); externalID != "" {
		_, err = proxy.journal.UpsertCaptureJob(repository.CaptureJob{
			SessionID:   proxy.session.ID,
			Provider:    proxy.platform,
			ExternalID:  externalID,
			Status:      status,
			RequestOID:  captured.requestOID,
			ResponseOID: responseOID,
			StartedAt:   captured.startedAt,
			Metadata:    mustProxyJSON(map[string]any{"event_id": event.ID, "path": captured.path}),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (proxy *Proxy) handleProxyError(writer http.ResponseWriter, request *http.Request, proxyErr error) {
	if captured, ok := request.Context().Value(proxyCaptureKey{}).(*proxyCapture); ok {
		_, _ = proxy.journal.AppendCaptureEvent(repository.CaptureEvent{
			SessionID:  proxy.session.ID,
			EventType:  proxyEventType(proxy.platform, captured.method, captured.path),
			OccurredAt: captured.startedAt,
			Fidelity:   recipe.FidelityExactRequest,
			Normalized: mustProxyJSON(map[string]any{
				"platform": proxy.platform,
				"method":   captured.method,
				"path":     captured.path,
				"error":    proxyErr.Error(),
			}),
		})
	}
	writeAPIError(writer, http.StatusBadGateway, errors.New("upstream request failed"))
}

func proxyEventType(platform, method, path string) string {
	lowerPath := strings.ToLower(path)
	switch platform {
	case "comfyui":
		if method == http.MethodPost && strings.HasPrefix(lowerPath, "/prompt") {
			return "comfyui.prompt"
		}
		if strings.HasPrefix(lowerPath, "/history") {
			return "comfyui.history"
		}
	case "automatic1111":
		if strings.Contains(lowerPath, "txt2img") {
			return "automatic1111.txt2img"
		}
		if strings.Contains(lowerPath, "img2img") {
			return "automatic1111.img2img"
		}
	case "openai", "firefly":
		return platform + ".request"
	}
	return platform + ".http"
}

func proxyJob(body []byte, platform string, statusCode int) (string, string) {
	if len(body) == 0 || !json.Valid(body) {
		return "", ""
	}
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		return "", ""
	}
	keys := []string{"jobId", "job_id", "prompt_id"}
	if platform == "openai" {
		keys = append(keys, "id")
	}
	externalID := ""
	for _, key := range keys {
		if value, ok := document[key].(string); ok && value != "" {
			externalID = value
			break
		}
	}
	if externalID == "" {
		return "", ""
	}
	status := "succeeded"
	if value, ok := document["status"].(string); ok {
		switch strings.ToLower(value) {
		case "queued", "pending":
			status = "queued"
		case "running", "processing", "in_progress":
			status = "running"
		case "failed", "error":
			status = "failed"
		case "canceled", "cancelled":
			status = "canceled"
		}
	} else if statusCode >= 400 {
		status = "failed"
	}
	return externalID, status
}

func mustProxyJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
