package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zhao-xuan/PixLog/internal/imaging"
)

const PolicySchema = "pixlog.policy/v1"

type Policy struct {
	Schema string       `json:"schema"`
	Rules  []PolicyRule `json:"rules"`
}

type PolicyRule struct {
	Asset                string                `json:"asset"`
	AllowedFormats       []string              `json:"allowed_formats,omitempty"`
	MaxFileSize          *int64                `json:"max_file_size,omitempty"`
	RequireRecipe        bool                  `json:"require_recipe,omitempty"`
	MaxVisualChange      *float64              `json:"max_visual_change,omitempty"`
	MinSSIM              *float64              `json:"min_ssim,omitempty"`
	AllowedChangeRegions []imaging.BoundingBox `json:"allowed_change_regions,omitempty"`
	AllowedChangeMask    string                `json:"allowed_change_mask,omitempty"`
}

type PolicyViolation struct {
	Asset   string `json:"asset"`
	Rule    int    `json:"rule"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PolicyCheckResult struct {
	Policy        string            `json:"policy"`
	Passed        bool              `json:"passed"`
	CheckedAssets []string          `json:"checked_assets"`
	Violations    []PolicyViolation `json:"violations"`
}

func LoadPolicy(filePath string) (Policy, error) {
	file, err := os.Open(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return Policy{}, fmt.Errorf("policy file %s does not exist", filePath)
	}
	if err != nil {
		return Policy{}, fmt.Errorf("open policy: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var policy Policy
	if err := decoder.Decode(&policy); err != nil {
		return Policy{}, fmt.Errorf("decode policy: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Policy{}, errors.New("decode policy: expected one JSON document")
	}
	if err := validatePolicy(policy); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func (r *Repository) CheckPolicy(filePath string, options imaging.DiffOptions) (PolicyCheckResult, error) {
	absolutePolicyPath, err := filepath.Abs(filePath)
	if err != nil {
		return PolicyCheckResult{}, fmt.Errorf("resolve policy path: %w", err)
	}
	policy, err := LoadPolicy(absolutePolicyPath)
	if err != nil {
		return PolicyCheckResult{}, err
	}
	index, err := r.ReadIndex()
	if err != nil {
		return PolicyCheckResult{}, err
	}
	diff, err := r.DiffStaged(nil, options)
	if err != nil {
		return PolicyCheckResult{}, err
	}
	return evaluatePolicy(absolutePolicyPath, policy, index.Entries, diff, r.Root, r.Load, options)
}

func evaluatePolicy(
	absolutePolicyPath string,
	policy Policy,
	entries map[string]Entry,
	diff DiffReport,
	root string,
	loadObject func(string) ([]byte, error),
	options imaging.DiffOptions,
) (PolicyCheckResult, error) {
	changed := make(map[string]AssetDiff, len(diff.Assets))
	assets := make(map[string]struct{}, len(entries)+len(diff.Assets))
	for asset := range entries {
		assets[asset] = struct{}{}
	}
	for _, assetDiff := range diff.Assets {
		changed[assetDiff.Path] = assetDiff
		assets[assetDiff.Path] = struct{}{}
	}

	checked := map[string]struct{}{}
	violations := []PolicyViolation{}
	orderedAssets := sortedKeys(assets)
	for ruleIndex, rule := range policy.Rules {
		for _, asset := range orderedAssets {
			if !matchesPolicyAsset(rule.Asset, asset) {
				continue
			}
			checked[asset] = struct{}{}
			entry, exists := entries[asset]
			if exists {
				violations = append(violations, checkEntryPolicy(ruleIndex, rule, entry)...)
			}
			if assetDiff, changedAsset := changed[asset]; changedAsset {
				violations = append(violations, checkVisualPolicy(ruleIndex, rule, assetDiff)...)
				maskViolations, err := checkMaskPolicy(root, loadObject, ruleIndex, rule, assetDiff, options)
				if err != nil {
					return PolicyCheckResult{}, err
				}
				violations = append(violations, maskViolations...)
			}
		}
	}
	sortViolations(violations)

	result := PolicyCheckResult{
		Policy:        absolutePolicyPath,
		Passed:        len(violations) == 0,
		CheckedAssets: sortedKeys(checked),
		Violations:    violations,
	}
	return result, nil
}

func validatePolicy(policy Policy) error {
	if policy.Schema != PolicySchema {
		return fmt.Errorf("unsupported policy schema %q; expected %s", policy.Schema, PolicySchema)
	}
	if len(policy.Rules) == 0 {
		return errors.New("policy must contain at least one rule")
	}
	for index, rule := range policy.Rules {
		if strings.TrimSpace(rule.Asset) == "" {
			return fmt.Errorf("policy rule %d: asset cannot be empty", index+1)
		}
		if filepath.IsAbs(rule.Asset) || rule.Asset == ".." || strings.HasPrefix(rule.Asset, "../") {
			return fmt.Errorf("policy rule %d: asset must be repository-relative", index+1)
		}
		if _, err := path.Match(rule.Asset, rule.Asset); err != nil {
			return fmt.Errorf("policy rule %d: invalid asset pattern: %w", index+1, err)
		}
		if rule.MaxFileSize != nil && *rule.MaxFileSize < 0 {
			return fmt.Errorf("policy rule %d: max_file_size cannot be negative", index+1)
		}
		if rule.MaxVisualChange != nil && (*rule.MaxVisualChange < 0 || *rule.MaxVisualChange > 1) {
			return fmt.Errorf("policy rule %d: max_visual_change must be between 0 and 1", index+1)
		}
		if rule.MinSSIM != nil && (*rule.MinSSIM < 0 || *rule.MinSSIM > 1) {
			return fmt.Errorf("policy rule %d: min_ssim must be between 0 and 1", index+1)
		}
		for _, region := range rule.AllowedChangeRegions {
			if region.X < 0 || region.Y < 0 || region.Width <= 0 || region.Height <= 0 {
				return fmt.Errorf("policy rule %d: allowed change regions require non-negative coordinates and positive dimensions", index+1)
			}
		}
		if rule.AllowedChangeMask != "" {
			maskPath := path.Clean(filepath.ToSlash(rule.AllowedChangeMask))
			if path.IsAbs(maskPath) || maskPath == ".." || strings.HasPrefix(maskPath, "../") {
				return fmt.Errorf("policy rule %d: allowed_change_mask must be repository-relative", index+1)
			}
		}
	}
	return nil
}

func checkEntryPolicy(ruleIndex int, rule PolicyRule, entry Entry) []PolicyViolation {
	violations := []PolicyViolation{}
	add := func(code, message string) {
		violations = append(violations, PolicyViolation{Asset: entry.Path, Rule: ruleIndex + 1, Code: code, Message: message})
	}
	if len(rule.AllowedFormats) > 0 && !containsFold(rule.AllowedFormats, entry.Format) {
		add("format_not_allowed", fmt.Sprintf("format %s is not one of %s", entry.Format, strings.Join(rule.AllowedFormats, ", ")))
	}
	if rule.MaxFileSize != nil && entry.Size > *rule.MaxFileSize {
		add("file_too_large", fmt.Sprintf("file size %d exceeds %d bytes", entry.Size, *rule.MaxFileSize))
	}
	if rule.RequireRecipe && entry.RecipeOID == "" {
		add("recipe_required", "asset does not have a generation recipe")
	}
	return violations
}

func checkVisualPolicy(ruleIndex int, rule PolicyRule, asset AssetDiff) []PolicyViolation {
	if rule.MaxVisualChange == nil && rule.MinSSIM == nil && len(rule.AllowedChangeRegions) == 0 {
		return nil
	}
	violation := func(code, message string) PolicyViolation {
		return PolicyViolation{Asset: asset.Path, Rule: ruleIndex + 1, Code: code, Message: message}
	}

	if asset.Old == nil || asset.New == nil {
		if rule.MaxVisualChange != nil && *rule.MaxVisualChange < 1 {
			return []PolicyViolation{violation("max_visual_change", fmt.Sprintf("asset %s counts as 100%% visual change", asset.Kind))}
		}
		if rule.MinSSIM != nil || len(rule.AllowedChangeRegions) > 0 {
			return []PolicyViolation{violation("visual_diff_unavailable", fmt.Sprintf("cannot evaluate %s asset against visual constraints", asset.Kind))}
		}
		return nil
	}
	if asset.Old.ContentOID == asset.New.ContentOID {
		return nil
	}
	if asset.Visual == nil || !asset.Visual.Comparable {
		return []PolicyViolation{violation("visual_diff_unavailable", "changed asset cannot be decoded by the built-in visual diff engine")}
	}

	violations := []PolicyViolation{}
	if rule.MaxVisualChange != nil && asset.Visual.VisualChangeRatio > *rule.MaxVisualChange {
		violations = append(violations, violation("max_visual_change", fmt.Sprintf("visual change %.6f exceeds %.6f", asset.Visual.VisualChangeRatio, *rule.MaxVisualChange)))
	}
	if rule.MinSSIM != nil && asset.Visual.SSIM < *rule.MinSSIM {
		violations = append(violations, violation("min_ssim", fmt.Sprintf("SSIM %.6f is below %.6f", asset.Visual.SSIM, *rule.MinSSIM)))
	}
	if len(rule.AllowedChangeRegions) > 0 {
		for _, changedRegion := range asset.Visual.Regions {
			if !containedByAny(changedRegion.BoundingBox, rule.AllowedChangeRegions) {
				box := changedRegion.BoundingBox
				violations = append(violations, violation("change_outside_allowed_region", fmt.Sprintf("changed region %d,%d %dx%d is outside allowed regions", box.X, box.Y, box.Width, box.Height)))
			}
		}
	}
	return violations
}

func checkMaskPolicy(root string, loadObject func(string) ([]byte, error), ruleIndex int, rule PolicyRule, asset AssetDiff, options imaging.DiffOptions) ([]PolicyViolation, error) {
	if rule.AllowedChangeMask == "" || (asset.Old != nil && asset.New != nil && asset.Old.ContentOID == asset.New.ContentOID) {
		return nil, nil
	}
	violation := func(code, message string) PolicyViolation {
		return PolicyViolation{Asset: asset.Path, Rule: ruleIndex + 1, Code: code, Message: message}
	}
	if asset.Old == nil || asset.New == nil {
		return []PolicyViolation{violation("visual_diff_unavailable", fmt.Sprintf("cannot evaluate %s asset against allowed-change mask", asset.Kind))}, nil
	}
	oldData, err := loadObject(asset.Old.ContentOID)
	if err != nil {
		return nil, err
	}
	newData, err := loadObject(asset.New.ContentOID)
	if err != nil {
		return nil, err
	}
	maskPath := filepath.Join(root, filepath.FromSlash(path.Clean(filepath.ToSlash(rule.AllowedChangeMask))))
	maskData, err := os.ReadFile(maskPath)
	if err != nil {
		return nil, fmt.Errorf("read allowed-change mask %s: %w", rule.AllowedChangeMask, err)
	}
	check, err := imaging.CheckAllowedMaskReaders(bytes.NewReader(oldData), bytes.NewReader(newData), bytes.NewReader(maskData), options)
	if err != nil {
		return nil, fmt.Errorf("check allowed-change mask for %s: %w", asset.Path, err)
	}
	if check.OutsidePixels == 0 {
		return nil, nil
	}
	return []PolicyViolation{violation(
		"change_outside_allowed_mask",
		fmt.Sprintf("%d of %d changed pixels are outside mask %s", check.OutsidePixels, check.ChangedPixels, rule.AllowedChangeMask),
	)}, nil
}

func matchesPolicyAsset(pattern, asset string) bool {
	matched, err := path.Match(filepath.ToSlash(pattern), asset)
	return err == nil && matched
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func containedByAny(actual imaging.BoundingBox, allowed []imaging.BoundingBox) bool {
	for _, candidate := range allowed {
		if actual.X >= candidate.X && actual.Y >= candidate.Y &&
			actual.X+actual.Width <= candidate.X+candidate.Width &&
			actual.Y+actual.Height <= candidate.Y+candidate.Height {
			return true
		}
	}
	return false
}

func sortViolations(violations []PolicyViolation) {
	sort.Slice(violations, func(left, right int) bool {
		if violations[left].Asset != violations[right].Asset {
			return violations[left].Asset < violations[right].Asset
		}
		if violations[left].Rule != violations[right].Rule {
			return violations[left].Rule < violations[right].Rule
		}
		return violations[left].Code < violations[right].Code
	})
}
