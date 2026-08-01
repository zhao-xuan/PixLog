package repository

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhao-xuan/PixLog/internal/imaging"
)

type BisectResult struct {
	Path       string    `json:"path"`
	Metric     string    `json:"metric"`
	Threshold  float64   `json:"threshold"`
	Found      bool      `json:"found"`
	CommitOID  string    `json:"commit_oid,omitempty"`
	Author     string    `json:"author,omitempty"`
	Message    string    `json:"message,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	Value      float64   `json:"value,omitempty"`
	ContentOID string    `json:"content_oid,omitempty"`
}

func (r *Repository) VisualBisect(assetPath, baselinePath, metric string, threshold float64, options imaging.DiffOptions) (BisectResult, error) {
	metric = strings.ToLower(strings.TrimSpace(metric))
	if metric != "ssim" && metric != "rmse" && metric != "change" {
		return BisectResult{}, errors.New("metric must be ssim, rmse, or change")
	}
	if threshold < 0 || threshold > 1 {
		return BisectResult{}, errors.New("metric threshold must be between 0 and 1")
	}
	absoluteAsset, err := filepath.Abs(assetPath)
	if err != nil {
		return BisectResult{}, err
	}
	path, err := r.relativePath(absoluteAsset)
	if err != nil {
		return BisectResult{}, err
	}
	baseline, err := os.ReadFile(baselinePath)
	if err != nil {
		return BisectResult{}, fmt.Errorf("read baseline: %w", err)
	}
	records, err := r.Log(assetPath, 0)
	if err != nil {
		return BisectResult{}, err
	}
	if len(records) == 0 {
		return BisectResult{}, fmt.Errorf("asset %s has no commit history", path)
	}

	result := BisectResult{Path: path, Metric: metric, Threshold: threshold}
	for index := len(records) - 1; index >= 0; index-- {
		record := records[index]
		entry, exists := record.Commit.Tree[path]
		value := worstMetricValue(metric)
		if exists {
			data, err := r.Load(entry.ContentOID)
			if err != nil {
				return BisectResult{}, err
			}
			visual, err := imaging.CompareReaders(bytes.NewReader(baseline), bytes.NewReader(data), options)
			if err != nil {
				return BisectResult{}, fmt.Errorf("compare baseline with %s: %w", ShortOID(record.OID), err)
			}
			switch metric {
			case "ssim":
				value = visual.SSIM
			case "rmse":
				value = visual.RMSE
			case "change":
				value = visual.VisualChangeRatio
			}
		}
		if metricCrossed(metric, value, threshold) {
			result.Found = true
			result.CommitOID = record.OID
			result.Author = record.Commit.Author
			result.Message = record.Commit.Message
			result.CreatedAt = record.Commit.CreatedAt
			result.Value = value
			if exists {
				result.ContentOID = entry.ContentOID
			}
			return result, nil
		}
	}
	return result, nil
}

func worstMetricValue(metric string) float64 {
	if metric == "ssim" {
		return 0
	}
	return 1
}

func metricCrossed(metric string, value, threshold float64) bool {
	if metric == "ssim" {
		return value < threshold
	}
	return value > threshold
}
