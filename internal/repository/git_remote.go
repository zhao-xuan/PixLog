package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type GitRefUpdate struct {
	LocalRef  string
	LocalOID  string
	RemoteRef string
	RemoteOID string
}

type MediaTransferResult struct {
	Endpoint string   `json:"endpoint"`
	Objects  []string `json:"objects"`
	Bytes    int64    `json:"bytes"`
}

type batchRequest struct {
	Operation string        `json:"operation"`
	Transfers []string      `json:"transfers,omitempty"`
	Objects   []batchObject `json:"objects"`
}

type batchResponse struct {
	Objects []batchObject `json:"objects"`
}

type batchObject struct {
	OID     string                 `json:"oid"`
	Size    int64                  `json:"size"`
	Actions map[string]batchAction `json:"actions,omitempty"`
	Error   *batchError            `json:"error,omitempty"`
}

type batchAction struct {
	Href   string            `json:"href"`
	Header map[string]string `json:"header,omitempty"`
}

type batchError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func ParseGitRefUpdates(data []byte) ([]GitRefUpdate, error) {
	updates := []GitRefUpdate{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 4 {
			return nil, fmt.Errorf("invalid Git pre-push update %q", line)
		}
		updates = append(updates, GitRefUpdate{LocalRef: fields[0], LocalOID: fields[1], RemoteRef: fields[2], RemoteOID: fields[3]})
	}
	return updates, nil
}

func (g *GitRepository) ReferencedMediaObjects(updates []GitRefUpdate) ([]string, error) {
	objectSet := map[string]struct{}{}
	for _, update := range updates {
		if isZeroOID(update.LocalOID) {
			continue
		}
		args := []string{"rev-list", "--objects", update.LocalOID}
		if !isZeroOID(update.RemoteOID) {
			if _, err := gitOutput(g.Root, "cat-file", "-e", update.RemoteOID+"^{commit}"); err == nil {
				args = append(args, "^"+update.RemoteOID)
			}
		}
		listing, err := gitOutput(g.Root, args...)
		if err != nil {
			return nil, fmt.Errorf("scan Git objects for %s: %w", update.LocalRef, err)
		}
		for _, line := range strings.Split(listing, "\n") {
			objectOID, _, hasPath := strings.Cut(line, " ")
			if !hasPath {
				continue
			}
			data, err := gitBytes(g.Root, "cat-file", "blob", objectOID)
			if err != nil {
				continue
			}
			pointer, found, err := ParsePixLogPointer(data)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			objectSet[pointer.OID] = struct{}{}
			objectSet[pointer.ManifestOID] = struct{}{}
			if pointer.RecipeOID != "" {
				objectSet[pointer.RecipeOID] = struct{}{}
			}
		}
	}
	objects := make([]string, 0, len(objectSet))
	for oid := range objectSet {
		objects = append(objects, oid)
	}
	sort.Strings(objects)
	return objects, nil
}

func (g *GitRepository) MediaEndpoint(remoteName string) (string, error) {
	if remoteName == "" {
		remoteName = "origin"
	}
	endpoint, err := gitOptionalOutput(g.Root, []int{1}, "config", "--get", "pixlog.remote."+remoteName+".endpoint")
	if err != nil {
		return "", err
	}
	if endpoint == "" {
		endpoint = readPixLogEndpoint(filepath.Join(g.Root, ".pixlog.toml"))
	}
	if endpoint != "" && endpoint != "auto" {
		return endpoint, nil
	}
	remoteURL, err := gitOutput(g.Root, "config", "--get", "remote."+remoteName+".url")
	if err != nil {
		return "", fmt.Errorf("resolve PixLog endpoint: configure pixlog.remote.%s.endpoint: %w", remoteName, err)
	}
	if strings.HasPrefix(remoteURL, "file://") {
		return remoteURL + ".pixlog", nil
	}
	if !strings.Contains(remoteURL, "://") && !strings.Contains(remoteURL, "@") {
		if !filepath.IsAbs(remoteURL) {
			remoteURL = filepath.Join(g.Root, remoteURL)
		}
		return filepath.Clean(remoteURL) + ".pixlog", nil
	}
	return "", fmt.Errorf("cannot infer PixLog media endpoint from %q; set git config pixlog.remote.%s.endpoint <url>", remoteURL, remoteName)
}

func (g *GitRepository) PushMediaObjects(remoteName string, updates []GitRefUpdate) (MediaTransferResult, error) {
	objects, err := g.ReferencedMediaObjects(updates)
	if err != nil {
		return MediaTransferResult{}, err
	}
	if len(objects) == 0 {
		return MediaTransferResult{Objects: []string{}}, nil
	}
	endpoint, err := g.MediaEndpoint(remoteName)
	if err != nil {
		return MediaTransferResult{}, err
	}
	store, err := OpenGitMediaStore(g.Root)
	if err != nil {
		return MediaTransferResult{}, err
	}
	result := MediaTransferResult{Endpoint: endpoint, Objects: objects}
	if isHTTPURL(endpoint) {
		result.Bytes, err = uploadHTTPBatch(endpoint, store, objects)
	} else {
		result.Bytes, err = uploadFileObjects(endpoint, store, objects)
	}
	return result, err
}

func (g *GitRepository) FetchMediaObject(remoteName, oid string) error {
	store, err := OpenGitMediaStore(g.Root)
	if err != nil {
		return err
	}
	if store.Has(oid) {
		return nil
	}
	endpoint, err := g.MediaEndpoint(remoteName)
	if err != nil {
		return err
	}
	if isHTTPURL(endpoint) {
		return downloadHTTPBatch(endpoint, store, oid)
	}
	return downloadFileObject(endpoint, store, oid)
}

func uploadFileObjects(endpoint string, store *GitMediaStore, objects []string) (int64, error) {
	root, err := fileEndpointPath(endpoint)
	if err != nil {
		return 0, err
	}
	var transferred int64
	for _, oid := range objects {
		source, err := store.ObjectPath(oid)
		if err != nil {
			return transferred, err
		}
		digest, _ := parseOID(oid)
		destination := filepath.Join(root, "objects", "sha256", digest[:2], digest[2:])
		if existing, err := os.ReadFile(destination); err == nil {
			if hashBytes(existing) != oid {
				return transferred, fmt.Errorf("remote PixLog object %s failed verification", oid)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return transferred, err
		}
		if err := copyVerifiedFile(source, destination, digest); err != nil {
			return transferred, err
		}
		info, err := os.Stat(source)
		if err != nil {
			return transferred, err
		}
		transferred += info.Size()
	}
	return transferred, nil
}

func downloadFileObject(endpoint string, store *GitMediaStore, oid string) error {
	root, err := fileEndpointPath(endpoint)
	if err != nil {
		return err
	}
	digest, err := parseOID(oid)
	if err != nil {
		return err
	}
	destination, err := store.ObjectPath(oid)
	if err != nil {
		return err
	}
	return copyVerifiedFile(filepath.Join(root, "objects", "sha256", digest[:2], digest[2:]), destination, digest)
}

func uploadHTTPBatch(endpoint string, store *GitMediaStore, objects []string) (int64, error) {
	requestObjects := make([]batchObject, 0, len(objects))
	for _, oid := range objects {
		data, err := store.Get(oid)
		if err != nil {
			return 0, err
		}
		requestObjects = append(requestObjects, batchObject{OID: strings.TrimPrefix(oid, "sha256:"), Size: int64(len(data))})
	}
	response, err := callBatch(endpoint, batchRequest{Operation: "upload", Transfers: []string{"basic"}, Objects: requestObjects})
	if err != nil {
		return 0, err
	}
	var transferred int64
	for _, object := range response.Objects {
		if object.Error != nil {
			return transferred, fmt.Errorf("remote rejected object %s: %s", object.OID, object.Error.Message)
		}
		action, upload := object.Actions["upload"]
		if !upload {
			continue
		}
		oid := "sha256:" + object.OID
		data, err := store.Get(oid)
		if err != nil {
			return transferred, err
		}
		if err := performHTTPAction(http.MethodPut, action, bytes.NewReader(data)); err != nil {
			return transferred, err
		}
		transferred += int64(len(data))
		if verify, exists := object.Actions["verify"]; exists {
			body, _ := json.Marshal(batchObject{OID: object.OID, Size: int64(len(data))})
			if err := performHTTPAction(http.MethodPost, verify, bytes.NewReader(body)); err != nil {
				return transferred, err
			}
		}
	}
	return transferred, nil
}

func downloadHTTPBatch(endpoint string, store *GitMediaStore, oid string) error {
	digest, err := parseOID(oid)
	if err != nil {
		return err
	}
	response, err := callBatch(endpoint, batchRequest{Operation: "download", Transfers: []string{"basic"}, Objects: []batchObject{{OID: digest}}})
	if err != nil {
		return err
	}
	if len(response.Objects) != 1 || response.Objects[0].Error != nil {
		return fmt.Errorf("remote did not provide PixLog object %s", oid)
	}
	action, exists := response.Objects[0].Actions["download"]
	if !exists {
		return fmt.Errorf("remote did not provide a download action for %s", oid)
	}
	request, err := http.NewRequest(http.MethodGet, action.Href, nil)
	if err != nil {
		return err
	}
	for key, value := range action.Header {
		request.Header.Set(key, value)
	}
	responseHTTP, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer responseHTTP.Body.Close()
	if responseHTTP.StatusCode < 200 || responseHTTP.StatusCode >= 300 {
		return fmt.Errorf("download %s: HTTP %s", oid, responseHTTP.Status)
	}
	data, err := io.ReadAll(responseHTTP.Body)
	if err != nil {
		return err
	}
	if hashBytes(data) != oid {
		return fmt.Errorf("downloaded PixLog object %s failed verification", oid)
	}
	_, err = store.Put(data)
	return err
}

func callBatch(endpoint string, payload batchRequest) (batchResponse, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return batchResponse{}, err
	}
	request, err := http.NewRequest(http.MethodPost, batchURL(endpoint), bytes.NewReader(data))
	if err != nil {
		return batchResponse{}, err
	}
	request.Header.Set("Accept", "application/vnd.git-lfs+json")
	request.Header.Set("Content-Type", "application/vnd.git-lfs+json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return batchResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return batchResponse{}, fmt.Errorf("PixLog batch request: HTTP %s", response.Status)
	}
	var result batchResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return batchResponse{}, err
	}
	return result, nil
}

func performHTTPAction(method string, action batchAction, body io.Reader) error {
	request, err := http.NewRequest(method, action.Href, body)
	if err != nil {
		return err
	}
	for key, value := range action.Header {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("PixLog object transfer: HTTP %s", response.Status)
	}
	return nil
}

func fileEndpointPath(endpoint string) (string, error) {
	if strings.HasPrefix(endpoint, "file://") {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return "", err
		}
		return filepath.Clean(parsed.Path), nil
	}
	if strings.Contains(endpoint, "://") {
		return "", fmt.Errorf("unsupported PixLog media endpoint %q", endpoint)
	}
	return filepath.Clean(endpoint), nil
}

func readPixLogEndpoint(configPath string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	inRemote := false
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "[") {
			inRemote = line == "[remote]"
			continue
		}
		if inRemote && strings.HasPrefix(line, "endpoint") {
			_, value, found := strings.Cut(line, "=")
			if found {
				return strings.Trim(strings.TrimSpace(value), "\"")
			}
		}
	}
	return ""
}

func batchURL(endpoint string) string {
	trimmed := strings.TrimRight(endpoint, "/")
	if strings.HasSuffix(trimmed, "/objects/batch") {
		return trimmed
	}
	return trimmed + "/objects/batch"
}

func isHTTPURL(endpoint string) bool {
	return strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://")
}

func isZeroOID(oid string) bool {
	return oid == "" || strings.Trim(oid, "0") == ""
}
