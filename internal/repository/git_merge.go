package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhao-xuan/PixLog/internal/recipe"
)

var ErrGitImageConflict = errors.New("image changes overlap or cannot be merged safely")

func (g *GitRepository) MergeDriver(basePath, oursPath, theirsPath, assetPath string) error {
	baseData, err := os.ReadFile(basePath)
	if err != nil {
		return err
	}
	oursData, err := os.ReadFile(oursPath)
	if err != nil {
		return err
	}
	theirsData, err := os.ReadFile(theirsPath)
	if err != nil {
		return err
	}
	if bytes.Equal(oursData, theirsData) || bytes.Equal(baseData, theirsData) {
		return nil
	}
	if bytes.Equal(baseData, oursData) {
		return writeFileAtomic(oursPath, theirsData, 0o644)
	}
	if strings.ToLower(filepath.Ext(assetPath)) != ".png" {
		return ErrGitImageConflict
	}
	baseImage, basePointer, err := g.decodeMergeImage(baseData)
	if err != nil {
		return err
	}
	oursImage, oursPointer, err := g.decodeMergeImage(oursData)
	if err != nil {
		return err
	}
	theirsImage, theirsPointer, err := g.decodeMergeImage(theirsData)
	if err != nil {
		return err
	}
	if baseImage.Bounds() != oursImage.Bounds() || baseImage.Bounds() != theirsImage.Bounds() {
		return ErrGitImageConflict
	}
	merged := image.NewNRGBA(baseImage.Bounds())
	for y := baseImage.Bounds().Min.Y; y < baseImage.Bounds().Max.Y; y++ {
		for x := baseImage.Bounds().Min.X; x < baseImage.Bounds().Max.X; x++ {
			baseColor := color.NRGBAModel.Convert(baseImage.At(x, y)).(color.NRGBA)
			oursColor := color.NRGBAModel.Convert(oursImage.At(x, y)).(color.NRGBA)
			theirsColor := color.NRGBAModel.Convert(theirsImage.At(x, y)).(color.NRGBA)
			oursChanged := oursColor != baseColor
			theirsChanged := theirsColor != baseColor
			if oursChanged && theirsChanged && oursColor != theirsColor {
				return ErrGitImageConflict
			}
			pixel := baseColor
			if oursChanged {
				pixel = oursColor
			}
			if theirsChanged {
				pixel = theirsColor
			}
			merged.SetNRGBA(x, y, pixel)
		}
	}
	var mergedData bytes.Buffer
	if err := png.Encode(&mergedData, merged); err != nil {
		return err
	}
	recipeDocument := map[string]any{
		"schema": recipe.Schema,
		"kind":   "merge",
		"capture": map[string]any{
			"source": "git-merge-driver",
		},
		"parents": []map[string]any{
			{"asset": oursPointer.OID, "role": "ours"},
			{"asset": theirsPointer.OID, "role": "theirs"},
			{"asset": basePointer.OID, "role": "base"},
		},
	}
	recipeData, err := json.Marshal(recipeDocument)
	if err != nil {
		return err
	}
	normalized, err := recipe.Normalize(recipeData)
	if err != nil {
		return err
	}
	store, err := OpenGitMediaStore(g.Root)
	if err != nil {
		return err
	}
	recipeOID, err := store.Put(normalized)
	if err != nil {
		return err
	}
	journal, err := OpenProvenanceJournal(g.Root)
	if err != nil {
		return err
	}
	defer journal.Close()
	if err := journal.Record(hashBytes(mergedData.Bytes()), recipeOID, assetPath); err != nil {
		return err
	}
	pointerData, _, err := g.CleanFilter(assetPath, mergedData.Bytes())
	if err != nil {
		return err
	}
	return writeFileAtomic(oursPath, pointerData, 0o644)
}

func (g *GitRepository) decodeMergeImage(data []byte) (image.Image, PixLogPointer, error) {
	pointer, found, err := ParsePixLogPointer(data)
	if err != nil {
		return nil, PixLogPointer{}, err
	}
	if found {
		data, pointer, err = g.SmudgeFilter(data)
		if err != nil {
			return nil, PixLogPointer{}, err
		}
	} else {
		pointer.OID = hashBytes(data)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	return decoded, pointer, err
}
