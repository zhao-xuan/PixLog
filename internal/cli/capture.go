package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/zhao-xuan/PixLog/internal/capture"
	"github.com/zhao-xuan/PixLog/internal/repository"
)

func runCapture(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pixlog capture <serve|status|token|guide|proxy|history|sessions|show|finalize> [options]")
	}
	switch args[0] {
	case "serve":
		return runCaptureServe(args[1:], stdout, stderr)
	case "status":
		return runCaptureStatus(args[1:], stdout, stderr)
	case "token":
		return runCaptureToken(args[1:], stdout, stderr)
	case "guide":
		return runCaptureGuide(args[1:], stdout, stderr)
	case "proxy":
		return runCaptureProxy(args[1:], stdout, stderr)
	case "history":
		return runCaptureHistory(args[1:], stdout, stderr)
	case "sessions":
		return runCaptureSessions(args[1:], stdout, stderr)
	case "show":
		return runCaptureShow(args[1:], stdout, stderr)
	case "finalize":
		return runCaptureFinalize(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown capture command %q", args[0])
	}
}

func runCaptureHistory(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("capture history", stderr)
	platform := flags.String("platform", "photoshop", "application that produced the history log")
	asJSON := flags.Bool("json", false, "emit machine-readable result")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("usage: pixlog capture history [--platform photoshop] [--json] <history-log> <asset>")
	}
	historyPath, assetPath := flags.Arg(0), flags.Arg(1)
	info, err := os.Stat(historyPath)
	if err != nil {
		return err
	}
	if info.Size() > 8<<20 {
		return errors.New("application history log exceeds 8 MiB")
	}
	logData, err := os.ReadFile(historyPath)
	if err != nil {
		return err
	}
	if len(logData) > 8<<20 {
		return errors.New("application history log exceeds 8 MiB")
	}
	outputOID, err := repository.HashFile(assetPath)
	if err != nil {
		return err
	}
	result, err := capture.ApplicationHistoryRecipe(*platform, outputOID, logData)
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
	payloadOID, err := store.Put(result.Payload)
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(result.Recipe, &document); err != nil {
		return err
	}
	document["vendor"] = map[string]any{"name": *platform, "raw_payload_oid": payloadOID}
	recipeData, err := json.Marshal(document)
	if err != nil {
		return err
	}
	recipeOID, err := repo.ImportRecipe(assetPath, recipeData)
	if err != nil {
		return err
	}
	output := map[string]any{"recipe_oid": recipeOID, "history_oid": payloadOID, "entries": result.Entries}
	if *asJSON {
		return writeJSON(stdout, output)
	}
	fmt.Fprintf(stdout, "Imported %d Photoshop history entries as recipe %s (history %s)\n", result.Entries, repository.ShortOID(recipeOID), repository.ShortOID(payloadOID))
	return nil
}

func runCaptureSessions(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("capture sessions", stderr)
	limit := flags.Int("limit", 20, "maximum sessions to return")
	asJSON := flags.Bool("json", false, "emit machine-readable sessions")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *limit <= 0 {
		return errors.New("usage: pixlog capture sessions [--limit <count>] [--json]")
	}
	journal, err := repository.OpenProvenanceJournal("")
	if err != nil {
		return err
	}
	defer journal.Close()
	sessions, err := journal.CaptureSessions(*limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, sessions)
	}
	for _, session := range sessions {
		status := "active"
		if session.EndedAt != nil {
			status = "finished"
		}
		fmt.Fprintf(stdout, "%-42s %-24s %-8s %s\n", session.ID, session.Adapter, status, session.StartedAt.Local().Format(time.RFC3339))
	}
	return nil
}

func runCaptureShow(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("capture show", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable session detail")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog capture show [--json] <session-id>")
	}
	detail, err := capture.LoadSession("", flags.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, detail)
	}
	fmt.Fprintf(stdout, "%s (%s %s)\n", detail.Session.ID, detail.Session.Adapter, detail.Session.AdapterVersion)
	fmt.Fprintf(stdout, "  events %d  jobs %d  checkpoints %d  artifacts %d\n", len(detail.Events), len(detail.Jobs), len(detail.Checkpoints), len(detail.Artifacts))
	for _, event := range detail.Events {
		fmt.Fprintf(stdout, "  %4d  %-28s %s\n", event.Sequence, event.EventType, event.Fidelity)
	}
	return nil
}

func runCaptureFinalize(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("capture finalize", stderr)
	kind := flags.String("kind", "", "override the canonical recipe kind")
	asJSON := flags.Bool("json", false, "emit machine-readable result")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("usage: pixlog capture finalize [--kind <kind>] [--json] <session-id> <asset>")
	}
	result, err := capture.FinalizeSession("", flags.Arg(0), flags.Arg(1), *kind)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Finalized session %s as recipe %s for %s\n", result.SessionID, repository.ShortOID(result.RecipeOID), result.AssetPath)
	return nil
}

func runCaptureProxy(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("capture proxy", stderr)
	platform := flags.String("platform", "", "capture platform")
	upstream := flags.String("upstream", "", "explicit upstream API origin")
	listen := flags.String("listen", "127.0.0.1:4780", "loopback listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *platform == "" || *upstream == "" {
		return errors.New("usage: pixlog capture proxy --platform <name> --upstream <url> [--listen 127.0.0.1:4780]")
	}
	if err := capture.ValidateLoopbackAddress(*listen); err != nil {
		return err
	}
	proxy, err := capture.NewProxy("", capture.ProxyOptions{Platform: *platform, Upstream: *upstream})
	if err != nil {
		return err
	}
	defer proxy.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Handler:           proxy.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	context, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-context.Done()
		shutdownContext, cancel := contextpkgWithTimeout(5 * time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownContext)
	}()
	fmt.Fprintf(stdout, "PixLog %s capture proxy listening on http://%s -> %s\n", *platform, *listen, *upstream)
	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runCaptureServe(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("capture serve", stderr)
	listen := flags.String("listen", capture.DefaultAddress, "loopback listen address")
	tokenEnv := flags.String("token-env", capture.CaptureTokenEnv, "environment variable containing the capture token")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: pixlog capture serve [--listen 127.0.0.1:4777] [--token-env PIXLOG_CAPTURE_TOKEN]")
	}
	if err := capture.ValidateLoopbackAddress(*listen); err != nil {
		return err
	}
	server, err := capture.NewServer("", capture.Options{Token: os.Getenv(*tokenEnv)})
	if err != nil {
		return err
	}
	defer server.Close()
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	context, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-context.Done()
		shutdownContext, cancel := contextpkgWithTimeout(5 * time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownContext)
	}()
	tokenStatus := "disabled; same-process and non-browser clients only"
	if os.Getenv(*tokenEnv) != "" {
		tokenStatus = "enabled from " + *tokenEnv
	}
	fmt.Fprintf(stdout, "PixLog capture daemon listening on http://%s (%s)\n", *listen, tokenStatus)
	err = httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func contextpkgWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

func runCaptureStatus(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("capture status", stderr)
	endpoint := flags.String("url", "http://"+capture.DefaultAddress, "capture daemon base URL")
	tokenEnv := flags.String("token-env", capture.CaptureTokenEnv, "environment variable containing the capture token")
	asJSON := flags.Bool("json", false, "emit machine-readable status")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: pixlog capture status [--url <daemon-url>] [--json]")
	}
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(*endpoint, "/")+"/v1/health", nil)
	if err != nil {
		return err
	}
	if token := os.Getenv(*tokenEnv); token != "" {
		request.Header.Set("X-PixLog-Token", token)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("connect to capture daemon: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("capture daemon returned %s", response.Status)
	}
	var status map[string]any
	if err := jsonNewDecoder(response.Body).Decode(&status); err != nil {
		return fmt.Errorf("decode capture daemon status: %w", err)
	}
	if *asJSON {
		return writeJSON(stdout, status)
	}
	fmt.Fprintf(stdout, "capture daemon: %v (%v)\n", status["status"], status["schema"])
	return nil
}

func jsonNewDecoder(reader io.Reader) interface{ Decode(any) error } {
	return jsonDecoder{reader: reader}
}

type jsonDecoder struct {
	reader io.Reader
}

func (decoder jsonDecoder) Decode(target any) error {
	return json.NewDecoder(decoder.reader).Decode(target)
}

func runCaptureToken(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("capture token", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: pixlog capture token")
	}
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Errorf("generate capture token: %w", err)
	}
	fmt.Fprintln(stdout, hex.EncodeToString(buffer))
	return nil
}

func runCaptureGuide(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("capture guide", stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable guidance")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		platforms := capture.Platforms()
		if *asJSON {
			return writeJSON(stdout, platforms)
		}
		fmt.Fprintf(stdout, "Available capture guides: %s\n", strings.Join(platforms, ", "))
		fmt.Fprintln(stdout, "Run `pixlog capture guide <platform>` for setup steps.")
		return nil
	}
	if flags.NArg() != 1 {
		return errors.New("usage: pixlog capture guide [--json] [platform]")
	}
	guide, err := capture.PlatformGuide(flags.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(stdout, guide)
	}
	fmt.Fprintf(stdout, "%s capture (%s)\n", guide.Platform, guide.Adapter)
	fmt.Fprintf(stdout, "Fidelity: %s\n", guide.CaptureFidelity)
	fmt.Fprintf(stdout, "Reproduction: %s\n\n%s\n\n", guide.Reproducibility, guide.Summary)
	for index, step := range guide.Steps {
		fmt.Fprintf(stdout, "%d. %s\n", index+1, step)
	}
	fmt.Fprintf(stdout, "\nVerify: %s\n", guide.VerificationCommand)
	return nil
}
