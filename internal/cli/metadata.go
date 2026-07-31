package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	c2paintegration "github.com/pixlog/pixlog/internal/c2pa"
	"github.com/pixlog/pixlog/internal/capture"
	"github.com/pixlog/pixlog/internal/imaging"
	"github.com/pixlog/pixlog/internal/recipe"
	"github.com/pixlog/pixlog/internal/repository"
)

func runMetadata(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pixlog metadata <inspect|import> [options] <asset>")
	}
	switch args[0] {
	case "inspect":
		return runMetadataInspect(args[1:], stdout, stderr)
	case "import":
		return runMetadataImport(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown metadata command %q", args[0])
	}
}

func runMetadataInspect(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("metadata inspect", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable metadata")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog metadata inspect [--json] <asset>")
	}
	manifest, err := inspectMetadataFile(flags.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, map[string]any{"path": filepath.ToSlash(flags.Arg(0)), "manifest": manifest})
	}
	fmt.Fprintf(stdout, "%s (%s, %d bytes)\n", flags.Arg(0), manifest.MediaType, manifest.Size)
	if len(manifest.EmbeddedMetadata) == 0 {
		fmt.Fprintln(stdout, "  no supported embedded metadata found")
		return nil
	}
	keys := make([]string, 0, len(manifest.EmbeddedMetadata))
	for key := range manifest.EmbeddedMetadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(stdout, "  %s: %s\n", key, manifest.EmbeddedMetadata[key])
	}
	return nil
}

func runMetadataImport(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("metadata import", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable result")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog metadata import [--json] <asset>")
	}
	assetPath := flags.Arg(0)
	manifest, err := inspectMetadataFile(assetPath)
	if err != nil {
		return err
	}
	if len(manifest.EmbeddedMetadata) == 0 {
		return fmt.Errorf("%s has no supported embedded metadata", assetPath)
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	store, err := repository.OpenGitMediaStore(repo.Root)
	if err != nil {
		return err
	}
	rawMetadata, err := json.Marshal(manifest.EmbeddedMetadata)
	if err != nil {
		return err
	}
	redactedMetadata, err := capture.RedactJSON(rawMetadata)
	if err != nil {
		return err
	}
	rawPayloadOID, err := store.Put(redactedMetadata)
	if err != nil {
		return err
	}
	document := map[string]any{}
	if embedded, found, embeddedErr := recipe.FromEmbedded(manifest.EmbeddedMetadata); embeddedErr != nil {
		return embeddedErr
	} else if found {
		if err := json.Unmarshal(embedded, &document); err != nil {
			return err
		}
	} else {
		document = map[string]any{
			"schema": recipe.Schema,
			"kind":   "provenance-import",
			"normalized": map[string]any{
				"operation": "provenance.metadata-import",
				"metadata":  manifest.EmbeddedMetadata,
			},
			"reproducibility": map[string]any{"status": recipe.ReproducibilityProvenanceOnly},
		}
	}
	captureData, _ := document["capture"].(map[string]any)
	if captureData == nil {
		captureData = map[string]any{}
		document["capture"] = captureData
	}
	captureData["adapter"] = "embedded-metadata"
	captureData["adapter_version"] = "1"
	captureData["fidelity"] = recipe.FidelityEmbeddedMetadata
	captureData["captured_at"] = time.Now().UTC()
	document["vendor"] = map[string]any{
		"name":            "embedded-metadata",
		"raw_payload_oid": rawPayloadOID,
	}
	recipeData, err := json.Marshal(document)
	if err != nil {
		return err
	}
	recipeOID, err := repo.ImportRecipe(assetPath, recipeData)
	if err != nil {
		return err
	}
	result := map[string]string{"recipe_oid": recipeOID, "raw_payload_oid": rawPayloadOID}
	if *asJSON {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Imported metadata recipe %s (raw %s)\n", repository.ShortOID(recipeOID), repository.ShortOID(rawPayloadOID))
	return nil
}

func runC2PA(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pixlog c2pa <verify|import|export|sign> [options] <asset>")
	}
	switch args[0] {
	case "verify":
		return runC2PAVerify(args[1:], stdout, stderr)
	case "import":
		return runC2PAImport(args[1:], stdout, stderr)
	case "export":
		return runC2PAExport(args[1:], stdout, stderr)
	case "sign":
		return runC2PASign(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown c2pa command %q", args[0])
	}
}

func runC2PAVerify(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("c2pa verify", stderr)
	toolPath := flags.String("tool", "c2patool", "path to the official c2patool executable")
	asJSON := flags.Bool("json", false, "emit c2patool JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog c2pa verify [--tool <path>] [--json] <asset>")
	}
	result, err := (c2paintegration.Tool{Executable: *toolPath}).Verify(flags.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		var document any
		if err := json.Unmarshal(result, &document); err != nil {
			return err
		}
		return writeJSON(stdout, document)
	}
	fmt.Fprintf(stdout, "C2PA credential verified by %s for %s\n", *toolPath, flags.Arg(0))
	return nil
}

func runC2PAImport(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("c2pa import", stderr)
	toolPath := flags.String("tool", "c2patool", "path to the official c2patool executable")
	asJSON := flags.Bool("json", false, "emit machine-readable result")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog c2pa import [--tool <path>] [--json] <asset>")
	}
	assetPath := flags.Arg(0)
	verification, err := (c2paintegration.Tool{Executable: *toolPath}).Verify(assetPath)
	if err != nil {
		return err
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	store, err := repository.OpenGitMediaStore(repo.Root)
	if err != nil {
		return err
	}
	redacted, err := capture.RedactJSON(verification)
	if err != nil {
		return err
	}
	rawPayloadOID, err := store.Put(redacted)
	if err != nil {
		return err
	}
	document := map[string]any{}
	if _, existing, existingErr := repo.RecipeData("", assetPath); existingErr == nil {
		if err := json.Unmarshal(existing, &document); err != nil {
			return err
		}
	} else {
		document = map[string]any{
			"schema": recipe.Schema,
			"kind":   "provenance-certificate",
			"capture": map[string]any{
				"adapter":         "c2patool",
				"adapter_version": "external",
				"fidelity":        recipe.FidelityEmbeddedMetadata,
				"captured_at":     time.Now().UTC(),
			},
			"reproducibility": map[string]any{"status": recipe.ReproducibilityProvenanceOnly},
		}
	}
	credential := map[string]any{
		"type":            "c2pa",
		"verified_at":     time.Now().UTC(),
		"verifier":        *toolPath,
		"raw_payload_oid": rawPayloadOID,
	}
	credentials, _ := document["credentials"].([]any)
	document["credentials"] = append(credentials, credential)
	recipeData, err := json.Marshal(document)
	if err != nil {
		return err
	}
	recipeOID, err := repo.ImportRecipe(assetPath, recipeData)
	if err != nil {
		return err
	}
	result := map[string]string{"recipe_oid": recipeOID, "credential_oid": rawPayloadOID}
	if *asJSON {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Imported verified C2PA credential %s into recipe %s\n", repository.ShortOID(rawPayloadOID), repository.ShortOID(recipeOID))
	return nil
}

func runC2PAExport(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("c2pa export", stderr)
	outputPath := flags.String("output", "", "C2PA manifest definition output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog c2pa export [--output <manifest.json>] <asset>")
	}
	assetPath := flags.Arg(0)
	if *outputPath == "" {
		*outputPath = assetPath + ".c2pa.json"
	}
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	_, recipeData, err := repo.RecipeData("", assetPath)
	if err != nil {
		return err
	}
	manifest, err := inspectMetadataFile(assetPath)
	if err != nil {
		return err
	}
	definition, err := c2paintegration.ManifestFromRecipe(assetPath, manifest.MediaType, Version, recipeData)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(definition, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(*outputPath, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Wrote public C2PA manifest definition to %s\n", *outputPath)
	return nil
}

func runC2PASign(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("c2pa sign", stderr)
	toolPath := flags.String("tool", "c2patool", "path to the official c2patool executable")
	manifestPath := flags.String("manifest", "", "existing C2PA manifest definition")
	outputPath := flags.String("output", "", "signed image output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 || *outputPath == "" {
		return errors.New("usage: pixlog c2pa sign --output <signed-asset> [--manifest <manifest.json>] [--tool <path>] <asset> [c2patool-options]")
	}
	assetPath := flags.Arg(0)
	generatedManifest := ""
	if *manifestPath == "" {
		file, err := os.CreateTemp("", "pixlog-c2pa-*.json")
		if err != nil {
			return err
		}
		generatedManifest = file.Name()
		if err := file.Close(); err != nil {
			return err
		}
		defer os.Remove(generatedManifest)
		if err := writeC2PAManifest(assetPath, generatedManifest); err != nil {
			return err
		}
		*manifestPath = generatedManifest
	}
	if err := (c2paintegration.Tool{Executable: *toolPath}).Sign(assetPath, *manifestPath, *outputPath, flags.Args()[1:]); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Wrote signed asset to %s\n", *outputPath)
	return nil
}

func writeC2PAManifest(assetPath, outputPath string) error {
	repo, err := repository.OpenGit("")
	if err != nil {
		return err
	}
	_, recipeData, err := repo.RecipeData("", assetPath)
	if err != nil {
		return err
	}
	manifest, err := inspectMetadataFile(assetPath)
	if err != nil {
		return err
	}
	definition, err := c2paintegration.ManifestFromRecipe(assetPath, manifest.MediaType, Version, recipeData)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(definition, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, append(data, '\n'), 0o600)
}

func inspectMetadataFile(assetPath string) (imaging.Manifest, error) {
	absolutePath, err := filepath.Abs(assetPath)
	if err != nil {
		return imaging.Manifest{}, err
	}
	contentOID, err := repository.HashFile(absolutePath)
	if err != nil {
		return imaging.Manifest{}, err
	}
	return imaging.InspectFile(absolutePath, contentOID)
}

func c2paToolMissingHint(err error) error {
	if err != nil && strings.Contains(err.Error(), "executable file not found") {
		return fmt.Errorf("%w; install the official c2patool or pass --tool <path>", err)
	}
	return err
}
