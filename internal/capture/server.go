package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pixlog/pixlog/internal/recipe"
	"github.com/pixlog/pixlog/internal/repository"
)

const (
	APISchema         = "pixlog.capture/v1"
	DefaultAddress    = "127.0.0.1:4777"
	CaptureTokenEnv   = "PIXLOG_CAPTURE_TOKEN"
	maxCaptureBody    = 32 << 20
	pixlogTokenHeader = "X-PixLog-Token"
)

type Options struct {
	Token string
}

type Server struct {
	store   *repository.GitMediaStore
	journal *repository.ProvenanceJournal
	token   string
	handler http.Handler
}

type eventIngestRequest struct {
	ID            string                 `json:"id,omitempty"`
	SessionID     string                 `json:"session_id"`
	Sequence      int64                  `json:"sequence,omitempty"`
	EventType     string                 `json:"event_type"`
	TransactionID string                 `json:"transaction_id,omitempty"`
	OccurredAt    time.Time              `json:"occurred_at,omitempty"`
	Fidelity      recipe.CaptureFidelity `json:"fidelity"`
	RawPayload    json.RawMessage        `json:"raw_payload,omitempty"`
	Normalized    json.RawMessage        `json:"normalized,omitempty"`
}

type recipeIngestRequest struct {
	ContentOID string          `json:"content_oid,omitempty"`
	AssetPath  string          `json:"asset_path,omitempty"`
	Recipe     json.RawMessage `json:"recipe"`
	RawPayload json.RawMessage `json:"raw_payload,omitempty"`
}

func NewServer(start string, options Options) (*Server, error) {
	store, err := repository.OpenGitMediaStore(start)
	if err != nil {
		return nil, err
	}
	journal, err := repository.OpenProvenanceJournal(start)
	if err != nil {
		return nil, err
	}
	server := &Server{store: store, journal: journal, token: options.Token}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", server.handleHealth)
	mux.HandleFunc("POST /v1/sessions", server.handleSession)
	mux.HandleFunc("POST /v1/events", server.handleEvent)
	mux.HandleFunc("POST /v1/jobs", server.handleJob)
	mux.HandleFunc("POST /v1/checkpoints", server.handleCheckpoint)
	mux.HandleFunc("POST /v1/artifacts", server.handleArtifact)
	mux.HandleFunc("POST /v1/recipes", server.handleRecipe)
	mux.HandleFunc("/v1/sessions/", server.handleSessionResource)
	server.handler = server.middleware(mux)
	return server, nil
}

func (server *Server) Handler() http.Handler {
	return server.handler
}

func (server *Server) Close() error {
	return server.journal.Close()
}

func (server *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if request.Method == http.MethodOptions {
			if server.token == "" {
				writeAPIError(writer, http.StatusForbidden, errors.New("cross-origin capture requires a token"))
				return
			}
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-PixLog-Token")
			writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		if request.Header.Get("Origin") != "" {
			if server.token == "" {
				writeAPIError(writer, http.StatusForbidden, errors.New("cross-origin capture requires a token"))
				return
			}
			writer.Header().Set("Access-Control-Allow-Origin", "*")
		}
		if server.token != "" && !server.authorized(request) {
			writeAPIError(writer, http.StatusUnauthorized, errors.New("invalid capture token"))
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) authorized(request *http.Request) bool {
	if request.Header.Get(pixlogTokenHeader) == server.token {
		return true
	}
	return strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ") == server.token
}

func (server *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeAPIJSON(writer, http.StatusOK, map[string]any{
		"schema": APISchema,
		"status": "ok",
	})
}

func (server *Server) handleSession(writer http.ResponseWriter, request *http.Request) {
	var session repository.CaptureSession
	if !decodeAPIJSON(writer, request, &session) {
		return
	}
	created, err := server.journal.StartCaptureSession(session)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeAPIJSON(writer, http.StatusCreated, created)
}

func (server *Server) handleSessionResource(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, "/v1/sessions/")
	if strings.HasSuffix(path, "/finish") && request.Method == http.MethodPost {
		id := strings.TrimSuffix(path, "/finish")
		if err := server.journal.FinishCaptureSession(id, time.Time{}); err != nil {
			writeAPIError(writer, http.StatusBadRequest, err)
			return
		}
		writeAPIJSON(writer, http.StatusOK, map[string]string{"id": id, "status": "finished"})
		return
	}
	if strings.HasSuffix(path, "/events") && request.Method == http.MethodGet {
		id := strings.TrimSuffix(path, "/events")
		events, err := server.journal.CaptureEvents(id)
		if err != nil {
			writeAPIError(writer, http.StatusBadRequest, err)
			return
		}
		writeAPIJSON(writer, http.StatusOK, events)
		return
	}
	writeAPIError(writer, http.StatusNotFound, errors.New("capture resource not found"))
}

func (server *Server) handleEvent(writer http.ResponseWriter, request *http.Request) {
	var input eventIngestRequest
	if !decodeAPIJSON(writer, request, &input) {
		return
	}
	rawPayloadOID, err := server.storeRawPayload(input.RawPayload)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	event, err := server.journal.AppendCaptureEvent(repository.CaptureEvent{
		ID:            input.ID,
		SessionID:     input.SessionID,
		Sequence:      input.Sequence,
		EventType:     input.EventType,
		TransactionID: input.TransactionID,
		OccurredAt:    input.OccurredAt,
		Fidelity:      input.Fidelity,
		RawPayloadOID: rawPayloadOID,
		Normalized:    input.Normalized,
	})
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeAPIJSON(writer, http.StatusCreated, event)
}

func (server *Server) handleJob(writer http.ResponseWriter, request *http.Request) {
	var job repository.CaptureJob
	if !decodeAPIJSON(writer, request, &job) {
		return
	}
	stored, err := server.journal.UpsertCaptureJob(job)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeAPIJSON(writer, http.StatusOK, stored)
}

func (server *Server) handleCheckpoint(writer http.ResponseWriter, request *http.Request) {
	var checkpoint repository.CaptureCheckpoint
	if !decodeAPIJSON(writer, request, &checkpoint) {
		return
	}
	stored, err := server.journal.RecordCaptureCheckpoint(checkpoint)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeAPIJSON(writer, http.StatusCreated, stored)
}

func (server *Server) handleArtifact(writer http.ResponseWriter, request *http.Request) {
	var artifact repository.CaptureArtifact
	if !decodeAPIJSON(writer, request, &artifact) {
		return
	}
	stored, err := server.journal.RecordCaptureArtifact(artifact)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	writeAPIJSON(writer, http.StatusCreated, stored)
}

func (server *Server) handleRecipe(writer http.ResponseWriter, request *http.Request) {
	var input recipeIngestRequest
	if !decodeAPIJSON(writer, request, &input) {
		return
	}
	if len(bytes.TrimSpace(input.Recipe)) == 0 {
		writeAPIError(writer, http.StatusBadRequest, errors.New("recipe is required"))
		return
	}
	var document map[string]any
	if err := json.Unmarshal(input.Recipe, &document); err != nil {
		writeAPIError(writer, http.StatusBadRequest, fmt.Errorf("decode recipe: %w", err))
		return
	}
	rawPayloadOID, err := server.storeRawPayload(input.RawPayload)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	if rawPayloadOID != "" {
		vendor, exists := document["vendor"].(map[string]any)
		if !exists {
			vendor = map[string]any{}
			document["vendor"] = vendor
		}
		vendor["raw_payload_oid"] = rawPayloadOID
	}
	recipeData, err := json.Marshal(document)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	normalized, err := recipe.Normalize(recipeData)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, err)
		return
	}
	recipeOID, err := server.store.Put(normalized)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, err)
		return
	}
	if input.ContentOID != "" {
		if input.AssetPath == "" {
			writeAPIError(writer, http.StatusBadRequest, errors.New("asset_path is required with content_oid"))
			return
		}
		if err := server.journal.Record(input.ContentOID, recipeOID, input.AssetPath); err != nil {
			writeAPIError(writer, http.StatusBadRequest, err)
			return
		}
	}
	writeAPIJSON(writer, http.StatusCreated, map[string]string{
		"recipe_oid":      recipeOID,
		"raw_payload_oid": rawPayloadOID,
	})
}

func (server *Server) storeRawPayload(payload json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(payload)) == 0 {
		return "", nil
	}
	redacted, err := RedactJSON(payload)
	if err != nil {
		return "", err
	}
	return server.store.Put(redacted)
}

func decodeAPIJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxCaptureBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAPIError(writer, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAPIError(writer, http.StatusBadRequest, errors.New("decode request: expected one JSON object"))
		return false
	}
	return true
}

func writeAPIJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeAPIError(writer http.ResponseWriter, status int, err error) {
	writeAPIJSON(writer, status, map[string]any{
		"schema": APISchema,
		"error":  err.Error(),
	})
}
