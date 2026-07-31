package repository

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pixlog/pixlog/internal/recipe"
)

type CaptureSession struct {
	ID             string          `json:"id"`
	Adapter        string          `json:"adapter"`
	AdapterVersion string          `json:"adapter_version"`
	Application    string          `json:"application,omitempty"`
	DocumentID     string          `json:"document_id,omitempty"`
	StartedAt      time.Time       `json:"started_at"`
	EndedAt        *time.Time      `json:"ended_at,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

type CaptureEvent struct {
	ID            string                 `json:"id"`
	SessionID     string                 `json:"session_id"`
	Sequence      int64                  `json:"sequence"`
	EventType     string                 `json:"event_type"`
	TransactionID string                 `json:"transaction_id,omitempty"`
	OccurredAt    time.Time              `json:"occurred_at"`
	Fidelity      recipe.CaptureFidelity `json:"fidelity"`
	RawPayloadOID string                 `json:"raw_payload_oid,omitempty"`
	Normalized    json.RawMessage        `json:"normalized,omitempty"`
}

type CaptureJob struct {
	ID          string          `json:"id"`
	SessionID   string          `json:"session_id,omitempty"`
	Provider    string          `json:"provider"`
	ExternalID  string          `json:"external_id"`
	Status      string          `json:"status"`
	RequestOID  string          `json:"request_oid,omitempty"`
	ResponseOID string          `json:"response_oid,omitempty"`
	StartedAt   time.Time       `json:"started_at"`
	FinishedAt  *time.Time      `json:"finished_at,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

type CaptureCheckpoint struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"session_id"`
	DocumentID string          `json:"document_id,omitempty"`
	Reason     string          `json:"reason"`
	ContentOID string          `json:"content_oid,omitempty"`
	RecipeOID  string          `json:"recipe_oid,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type CaptureArtifact struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id,omitempty"`
	JobID      string    `json:"job_id,omitempty"`
	EventID    string    `json:"event_id,omitempty"`
	Role       string    `json:"role"`
	ContentOID string    `json:"content_oid"`
	RecipeOID  string    `json:"recipe_oid,omitempty"`
	AssetPath  string    `json:"asset_path,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (journal *ProvenanceJournal) CaptureSession(id string) (CaptureSession, error) {
	row := journal.database.QueryRow(
		`SELECT id, adapter, adapter_version, application, document_id,
		 started_at, ended_at, metadata_json
		 FROM capture_sessions WHERE id = ?`, id,
	)
	session, err := scanCaptureSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CaptureSession{}, fmt.Errorf("capture session %s does not exist", id)
	}
	if err != nil {
		return CaptureSession{}, fmt.Errorf("query capture session: %w", err)
	}
	return session, nil
}

func (journal *ProvenanceJournal) CaptureSessions(limit int) ([]CaptureSession, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := journal.database.Query(
		`SELECT id, adapter, adapter_version, application, document_id,
		 started_at, ended_at, metadata_json
		 FROM capture_sessions ORDER BY started_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query capture sessions: %w", err)
	}
	defer rows.Close()
	sessions := []CaptureSession{}
	for rows.Next() {
		session, err := scanCaptureSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan capture session: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate capture sessions: %w", err)
	}
	return sessions, nil
}

func (journal *ProvenanceJournal) StartCaptureSession(session CaptureSession) (CaptureSession, error) {
	if strings.TrimSpace(session.Adapter) == "" {
		return CaptureSession{}, errors.New("capture session adapter is required")
	}
	if strings.TrimSpace(session.AdapterVersion) == "" {
		return CaptureSession{}, errors.New("capture session adapter version is required")
	}
	if session.ID == "" {
		session.ID = captureID("session")
	}
	if session.StartedAt.IsZero() {
		session.StartedAt = time.Now().UTC()
	}
	metadata, err := captureJSON(session.Metadata)
	if err != nil {
		return CaptureSession{}, fmt.Errorf("capture session metadata: %w", err)
	}
	_, err = journal.database.Exec(
		`INSERT INTO capture_sessions(
		 id, adapter, adapter_version, application, document_id, started_at, ended_at, metadata_json
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.Adapter, session.AdapterVersion, session.Application, session.DocumentID,
		captureTime(session.StartedAt), nullableCaptureTime(session.EndedAt), metadata,
	)
	if err != nil {
		return CaptureSession{}, fmt.Errorf("start capture session: %w", err)
	}
	session.Metadata = metadata
	return session, nil
}

func (journal *ProvenanceJournal) FinishCaptureSession(id string, endedAt time.Time) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("capture session ID is required")
	}
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}
	result, err := journal.database.Exec(
		"UPDATE capture_sessions SET ended_at = ? WHERE id = ?", captureTime(endedAt), id,
	)
	if err != nil {
		return fmt.Errorf("finish capture session: %w", err)
	}
	return requireCaptureRow(result, "capture session", id)
}

func (journal *ProvenanceJournal) AppendCaptureEvent(event CaptureEvent) (CaptureEvent, error) {
	if strings.TrimSpace(event.SessionID) == "" || strings.TrimSpace(event.EventType) == "" {
		return CaptureEvent{}, errors.New("capture event session_id and event_type are required")
	}
	if !event.Fidelity.Valid() {
		return CaptureEvent{}, fmt.Errorf("unsupported capture event fidelity %q", event.Fidelity)
	}
	if event.RawPayloadOID != "" {
		if _, err := parseOID(event.RawPayloadOID); err != nil {
			return CaptureEvent{}, fmt.Errorf("capture event raw payload: %w", err)
		}
	}
	normalized, err := captureJSON(event.Normalized)
	if err != nil {
		return CaptureEvent{}, fmt.Errorf("capture event normalized data: %w", err)
	}
	if event.ID == "" {
		event.ID = captureID("event")
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	transaction, err := journal.database.Begin()
	if err != nil {
		return CaptureEvent{}, fmt.Errorf("begin capture event: %w", err)
	}
	defer transaction.Rollback()
	if event.Sequence == 0 {
		if err := transaction.QueryRow(
			"SELECT COALESCE(MAX(sequence), 0) + 1 FROM capture_events WHERE session_id = ?",
			event.SessionID,
		).Scan(&event.Sequence); err != nil {
			return CaptureEvent{}, fmt.Errorf("allocate capture event sequence: %w", err)
		}
	}
	_, err = transaction.Exec(
		`INSERT INTO capture_events(
		 id, session_id, sequence, event_type, transaction_id, occurred_at, fidelity,
		 raw_payload_oid, normalized_json
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.SessionID, event.Sequence, event.EventType, event.TransactionID,
		captureTime(event.OccurredAt), event.Fidelity, event.RawPayloadOID, normalized,
	)
	if err != nil {
		return CaptureEvent{}, fmt.Errorf("append capture event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return CaptureEvent{}, fmt.Errorf("commit capture event: %w", err)
	}
	event.Normalized = normalized
	return event, nil
}

func (journal *ProvenanceJournal) CaptureEvents(sessionID string) ([]CaptureEvent, error) {
	rows, err := journal.database.Query(
		`SELECT id, session_id, sequence, event_type, transaction_id, occurred_at,
		 fidelity, raw_payload_oid, normalized_json
		 FROM capture_events WHERE session_id = ? ORDER BY sequence`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query capture events: %w", err)
	}
	defer rows.Close()
	events := []CaptureEvent{}
	for rows.Next() {
		var event CaptureEvent
		var occurredAt, fidelity string
		if err := rows.Scan(
			&event.ID, &event.SessionID, &event.Sequence, &event.EventType,
			&event.TransactionID, &occurredAt, &fidelity, &event.RawPayloadOID, &event.Normalized,
		); err != nil {
			return nil, fmt.Errorf("scan capture event: %w", err)
		}
		event.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("parse capture event time: %w", err)
		}
		event.Fidelity = recipe.CaptureFidelity(fidelity)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate capture events: %w", err)
	}
	return events, nil
}

func (journal *ProvenanceJournal) CaptureJobs(sessionID string) ([]CaptureJob, error) {
	rows, err := journal.database.Query(
		`SELECT id, COALESCE(session_id, ''), provider, external_id, status,
		 request_oid, response_oid, started_at, finished_at, metadata_json
		 FROM capture_jobs WHERE session_id = ? ORDER BY started_at`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query capture jobs: %w", err)
	}
	defer rows.Close()
	jobs := []CaptureJob{}
	for rows.Next() {
		var job CaptureJob
		var startedAt string
		var finishedAt sql.NullString
		if err := rows.Scan(
			&job.ID, &job.SessionID, &job.Provider, &job.ExternalID, &job.Status,
			&job.RequestOID, &job.ResponseOID, &startedAt, &finishedAt, &job.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan capture job: %w", err)
		}
		job.StartedAt, err = time.Parse(time.RFC3339Nano, startedAt)
		if err != nil {
			return nil, fmt.Errorf("parse capture job start time: %w", err)
		}
		if finishedAt.Valid {
			value, parseErr := time.Parse(time.RFC3339Nano, finishedAt.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse capture job finish time: %w", parseErr)
			}
			job.FinishedAt = &value
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate capture jobs: %w", err)
	}
	return jobs, nil
}

func (journal *ProvenanceJournal) CaptureCheckpoints(sessionID string) ([]CaptureCheckpoint, error) {
	rows, err := journal.database.Query(
		`SELECT id, session_id, document_id, reason, content_oid, recipe_oid,
		 created_at, metadata_json
		 FROM capture_checkpoints WHERE session_id = ? ORDER BY created_at`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query capture checkpoints: %w", err)
	}
	defer rows.Close()
	checkpoints := []CaptureCheckpoint{}
	for rows.Next() {
		var checkpoint CaptureCheckpoint
		var createdAt string
		if err := rows.Scan(
			&checkpoint.ID, &checkpoint.SessionID, &checkpoint.DocumentID, &checkpoint.Reason,
			&checkpoint.ContentOID, &checkpoint.RecipeOID, &createdAt, &checkpoint.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan capture checkpoint: %w", err)
		}
		checkpoint.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse capture checkpoint time: %w", err)
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate capture checkpoints: %w", err)
	}
	return checkpoints, nil
}

func (journal *ProvenanceJournal) CaptureArtifacts(sessionID string) ([]CaptureArtifact, error) {
	rows, err := journal.database.Query(
		`SELECT id, COALESCE(session_id, ''), COALESCE(job_id, ''), COALESCE(event_id, ''),
		 role, content_oid, recipe_oid, asset_path, created_at
		 FROM capture_artifacts WHERE session_id = ? ORDER BY created_at`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query capture artifacts: %w", err)
	}
	defer rows.Close()
	artifacts := []CaptureArtifact{}
	for rows.Next() {
		var artifact CaptureArtifact
		var createdAt string
		if err := rows.Scan(
			&artifact.ID, &artifact.SessionID, &artifact.JobID, &artifact.EventID, &artifact.Role,
			&artifact.ContentOID, &artifact.RecipeOID, &artifact.AssetPath, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan capture artifact: %w", err)
		}
		artifact.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse capture artifact time: %w", err)
		}
		artifacts = append(artifacts, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate capture artifacts: %w", err)
	}
	return artifacts, nil
}

func (journal *ProvenanceJournal) UpsertCaptureJob(job CaptureJob) (CaptureJob, error) {
	if strings.TrimSpace(job.Provider) == "" || strings.TrimSpace(job.ExternalID) == "" {
		return CaptureJob{}, errors.New("capture job provider and external_id are required")
	}
	if !validCaptureJobStatus(job.Status) {
		return CaptureJob{}, fmt.Errorf("unsupported capture job status %q", job.Status)
	}
	for label, oid := range map[string]string{"request": job.RequestOID, "response": job.ResponseOID} {
		if oid != "" {
			if _, err := parseOID(oid); err != nil {
				return CaptureJob{}, fmt.Errorf("capture job %s: %w", label, err)
			}
		}
	}
	metadata, err := captureJSON(job.Metadata)
	if err != nil {
		return CaptureJob{}, fmt.Errorf("capture job metadata: %w", err)
	}
	if job.ID == "" {
		job.ID = captureID("job")
	}
	if job.StartedAt.IsZero() {
		job.StartedAt = time.Now().UTC()
	}
	_, err = journal.database.Exec(
		`INSERT INTO capture_jobs(
		 id, session_id, provider, external_id, status, request_oid, response_oid,
		 started_at, finished_at, metadata_json
		) VALUES(?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, external_id) DO UPDATE SET
		 session_id = excluded.session_id,
		 status = excluded.status,
		 request_oid = excluded.request_oid,
		 response_oid = excluded.response_oid,
		 finished_at = excluded.finished_at,
		 metadata_json = excluded.metadata_json`,
		job.ID, job.SessionID, job.Provider, job.ExternalID, job.Status, job.RequestOID,
		job.ResponseOID, captureTime(job.StartedAt), nullableCaptureTime(job.FinishedAt), metadata,
	)
	if err != nil {
		return CaptureJob{}, fmt.Errorf("upsert capture job: %w", err)
	}
	job.Metadata = metadata
	return job, nil
}

func (journal *ProvenanceJournal) RecordCaptureCheckpoint(checkpoint CaptureCheckpoint) (CaptureCheckpoint, error) {
	if strings.TrimSpace(checkpoint.SessionID) == "" || strings.TrimSpace(checkpoint.Reason) == "" {
		return CaptureCheckpoint{}, errors.New("capture checkpoint session_id and reason are required")
	}
	for label, oid := range map[string]string{"content": checkpoint.ContentOID, "recipe": checkpoint.RecipeOID} {
		if oid != "" {
			if _, err := parseOID(oid); err != nil {
				return CaptureCheckpoint{}, fmt.Errorf("capture checkpoint %s: %w", label, err)
			}
		}
	}
	metadata, err := captureJSON(checkpoint.Metadata)
	if err != nil {
		return CaptureCheckpoint{}, fmt.Errorf("capture checkpoint metadata: %w", err)
	}
	if checkpoint.ID == "" {
		checkpoint.ID = captureID("checkpoint")
	}
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now().UTC()
	}
	_, err = journal.database.Exec(
		`INSERT INTO capture_checkpoints(
		 id, session_id, document_id, reason, content_oid, recipe_oid, created_at, metadata_json
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		checkpoint.ID, checkpoint.SessionID, checkpoint.DocumentID, checkpoint.Reason,
		checkpoint.ContentOID, checkpoint.RecipeOID, captureTime(checkpoint.CreatedAt), metadata,
	)
	if err != nil {
		return CaptureCheckpoint{}, fmt.Errorf("record capture checkpoint: %w", err)
	}
	checkpoint.Metadata = metadata
	return checkpoint, nil
}

func (journal *ProvenanceJournal) RecordCaptureArtifact(artifact CaptureArtifact) (CaptureArtifact, error) {
	if strings.TrimSpace(artifact.Role) == "" || artifact.ContentOID == "" {
		return CaptureArtifact{}, errors.New("capture artifact role and content_oid are required")
	}
	if _, err := parseOID(artifact.ContentOID); err != nil {
		return CaptureArtifact{}, fmt.Errorf("capture artifact content: %w", err)
	}
	if artifact.RecipeOID != "" {
		if _, err := parseOID(artifact.RecipeOID); err != nil {
			return CaptureArtifact{}, fmt.Errorf("capture artifact recipe: %w", err)
		}
	}
	if artifact.ID == "" {
		artifact.ID = captureID("artifact")
	}
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = time.Now().UTC()
	}
	_, err := journal.database.Exec(
		`INSERT INTO capture_artifacts(
		 id, session_id, job_id, event_id, role, content_oid, recipe_oid, asset_path, created_at
		) VALUES(?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?)`,
		artifact.ID, artifact.SessionID, artifact.JobID, artifact.EventID, artifact.Role,
		artifact.ContentOID, artifact.RecipeOID, artifact.AssetPath, captureTime(artifact.CreatedAt),
	)
	if err != nil {
		return CaptureArtifact{}, fmt.Errorf("record capture artifact: %w", err)
	}
	return artifact, nil
}

func captureJSON(data json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return []byte("{}"), nil
	}
	if !json.Valid(data) {
		return nil, errors.New("must be valid JSON")
	}
	var normalized bytes.Buffer
	if err := json.Compact(&normalized, data); err != nil {
		return nil, err
	}
	return normalized.Bytes(), nil
}

func captureID(prefix string) string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic(fmt.Sprintf("generate capture ID: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}

func captureTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableCaptureTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return captureTime(*value)
}

func requireCaptureRow(result sql.Result, kind, id string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("%s %s does not exist", kind, id)
	}
	return nil
}

func validCaptureJobStatus(status string) bool {
	switch status {
	case "queued", "running", "succeeded", "failed", "canceled":
		return true
	default:
		return false
	}
}

type captureSessionScanner interface {
	Scan(...any) error
}

func scanCaptureSession(scanner captureSessionScanner) (CaptureSession, error) {
	var session CaptureSession
	var startedAt string
	var endedAt sql.NullString
	if err := scanner.Scan(
		&session.ID, &session.Adapter, &session.AdapterVersion, &session.Application,
		&session.DocumentID, &startedAt, &endedAt, &session.Metadata,
	); err != nil {
		return CaptureSession{}, err
	}
	var err error
	session.StartedAt, err = time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return CaptureSession{}, fmt.Errorf("parse capture session start time: %w", err)
	}
	if endedAt.Valid {
		value, parseErr := time.Parse(time.RFC3339Nano, endedAt.String)
		if parseErr != nil {
			return CaptureSession{}, fmt.Errorf("parse capture session finish time: %w", parseErr)
		}
		session.EndedAt = &value
	}
	return session, nil
}
