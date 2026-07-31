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

	"github.com/pixlog/pixlog/internal/capture"
)

func runCapture(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: pixlog capture <serve|status|token|guide|proxy> [options]")
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
	default:
		return fmt.Errorf("unknown capture command %q", args[0])
	}
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
