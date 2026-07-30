package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type ProvenanceJournal struct {
	database *sql.DB
}

func OpenProvenanceJournal(start string) (*ProvenanceJournal, error) {
	store, err := OpenGitMediaStore(start)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(store.Root, 0o755); err != nil {
		return nil, fmt.Errorf("create PixLog Git state: %w", err)
	}
	database, err := sql.Open("sqlite", filepath.Join(store.Root, "journal.sqlite"))
	if err != nil {
		return nil, fmt.Errorf("open PixLog provenance journal: %w", err)
	}
	journal := &ProvenanceJournal{database: database}
	for _, statement := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		`CREATE TABLE IF NOT EXISTS provenance (
			content_oid TEXT PRIMARY KEY,
			recipe_oid TEXT NOT NULL,
			asset_path TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			database.Close()
			return nil, fmt.Errorf("initialize PixLog provenance journal: %w", err)
		}
	}
	return journal, nil
}

func (journal *ProvenanceJournal) Close() error {
	return journal.database.Close()
}

func (journal *ProvenanceJournal) Record(contentOID, recipeOID, assetPath string) error {
	if _, err := parseOID(contentOID); err != nil {
		return err
	}
	if _, err := parseOID(recipeOID); err != nil {
		return err
	}
	_, err := journal.database.Exec(
		`INSERT INTO provenance(content_oid, recipe_oid, asset_path, updated_at)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(content_oid) DO UPDATE SET
		 recipe_oid = excluded.recipe_oid,
		 asset_path = excluded.asset_path,
		 updated_at = excluded.updated_at`,
		contentOID, recipeOID, filepath.ToSlash(assetPath), time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("record PixLog provenance: %w", err)
	}
	return nil
}

func (journal *ProvenanceJournal) RecipeForContent(contentOID string) (string, bool, error) {
	if _, err := parseOID(contentOID); err != nil {
		return "", false, err
	}
	var recipeOID string
	err := journal.database.QueryRow("SELECT recipe_oid FROM provenance WHERE content_oid = ?", contentOID).Scan(&recipeOID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query PixLog provenance: %w", err)
	}
	if _, err := parseOID(recipeOID); err != nil {
		return "", false, err
	}
	return recipeOID, true, nil
}
