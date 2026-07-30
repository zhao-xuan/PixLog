package repository

import "path/filepath"

func (r *Repository) RootPath() string {
	return r.Root
}

func (r *Repository) CaptureEntries() (map[string]Entry, error) {
	index, err := r.ReadIndex()
	if err != nil {
		return nil, err
	}
	return index.Entries, nil
}

func (r *Repository) ApplyCommandCapture(modified, deleted []string, recipeData []byte) (string, error) {
	recipeOID := ""
	var err error
	if len(modified) > 0 {
		recipeOID, err = r.StoreRecipe(recipeData)
		if err != nil {
			return "", err
		}
		absolutePaths := make([]string, 0, len(modified))
		for _, path := range modified {
			absolutePaths = append(absolutePaths, filepath.Join(r.Root, filepath.FromSlash(path)))
		}
		if _, err := r.Add(absolutePaths, recipeOID); err != nil {
			return "", err
		}
	}
	if len(deleted) > 0 {
		absolutePaths := make([]string, 0, len(deleted))
		for _, path := range deleted {
			absolutePaths = append(absolutePaths, filepath.Join(r.Root, filepath.FromSlash(path)))
		}
		if _, err := r.Remove(absolutePaths); err != nil {
			return "", err
		}
	}
	return recipeOID, nil
}
