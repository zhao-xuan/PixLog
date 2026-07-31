package c2pa

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type Tool struct {
	Executable string
}

func (tool Tool) Verify(assetPath string) (json.RawMessage, error) {
	executable := tool.Executable
	if executable == "" {
		executable = "c2patool"
	}
	command := exec.Command(executable, assetPath, "--detailed")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("verify C2PA credential: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	data := bytes.TrimSpace(stdout.Bytes())
	if !json.Valid(data) {
		return nil, fmt.Errorf("c2patool returned invalid JSON: %s", strings.TrimSpace(stdout.String()))
	}
	return append(json.RawMessage(nil), data...), nil
}

func (tool Tool) Sign(assetPath, manifestPath, outputPath string, extraArguments []string) error {
	executable := tool.Executable
	if executable == "" {
		executable = "c2patool"
	}
	arguments := []string{assetPath, "--manifest", manifestPath, "--output", outputPath}
	arguments = append(arguments, extraArguments...)
	command := exec.Command(executable, arguments...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if output, err := command.Output(); err != nil {
		return fmt.Errorf("sign C2PA credential: %w: %s%s", err, strings.TrimSpace(stderr.String()), strings.TrimSpace(string(output)))
	}
	return nil
}
