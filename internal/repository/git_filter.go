package repository

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const maxPacketPayload = 65516

type packetReader struct {
	reader *bufio.Reader
}

func ServeGitFilterProcess(start string, input io.Reader, output io.Writer) error {
	repo, err := OpenGit(start)
	if err != nil {
		return err
	}
	reader := packetReader{reader: bufio.NewReader(input)}
	if err := filterHandshake(&reader, output); err != nil {
		return err
	}
	for {
		headers, err := reader.readList()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(headers) == 0 {
			continue
		}
		content, err := reader.readContent()
		if err != nil {
			return err
		}
		command := headers["command"]
		pathname := headers["pathname"]
		var result []byte
		switch command {
		case "clean":
			result, _, err = repo.CleanFilter(pathname, content)
		case "smudge":
			result, _, err = repo.SmudgeFilter(content)
		default:
			err = fmt.Errorf("unsupported Git filter command %q", command)
		}
		if err != nil {
			if responseErr := writeFilterResponse(output, "error", nil); responseErr != nil {
				return responseErr
			}
			continue
		}
		if err := writeFilterResponse(output, "success", result); err != nil {
			return err
		}
	}
}

func filterHandshake(reader *packetReader, output io.Writer) error {
	greeting, err := reader.readLines()
	if err != nil {
		return fmt.Errorf("read Git filter greeting: %w", err)
	}
	if len(greeting) != 2 || greeting[0] != "git-filter-client" || greeting[1] != "version=2" {
		return fmt.Errorf("unsupported Git filter greeting %q", greeting)
	}
	if err := writePacket(output, []byte("git-filter-server\n")); err != nil {
		return err
	}
	if err := writePacket(output, []byte("version=2\n")); err != nil {
		return err
	}
	if err := writeFlush(output); err != nil {
		return err
	}
	capabilities, err := reader.readLines()
	if err != nil {
		return fmt.Errorf("read Git filter capabilities: %w", err)
	}
	available := map[string]bool{}
	for _, capability := range capabilities {
		available[capability] = true
	}
	for _, capability := range []string{"capability=clean", "capability=smudge"} {
		if available[capability] {
			if err := writePacket(output, []byte(capability+"\n")); err != nil {
				return err
			}
		}
	}
	return writeFlush(output)
}

func (reader *packetReader) readList() (map[string]string, error) {
	lines, err := reader.readLines()
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(lines))
	for _, line := range lines {
		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("invalid Git filter header %q", line)
		}
		values[key] = value
	}
	return values, nil
}

func (reader *packetReader) readLines() ([]string, error) {
	lines := []string{}
	for {
		packet, flush, err := reader.readPacket()
		if err != nil {
			return nil, err
		}
		if flush {
			return lines, nil
		}
		lines = append(lines, strings.TrimSuffix(string(packet), "\n"))
	}
}

func (reader *packetReader) readContent() ([]byte, error) {
	var content []byte
	for {
		packet, flush, err := reader.readPacket()
		if err != nil {
			return nil, err
		}
		if flush {
			return content, nil
		}
		content = append(content, packet...)
	}
}

func (reader *packetReader) readPacket() ([]byte, bool, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader.reader, header); err != nil {
		return nil, false, err
	}
	length, err := strconv.ParseUint(string(header), 16, 16)
	if err != nil {
		return nil, false, fmt.Errorf("invalid packet header %q", header)
	}
	if length == 0 {
		return nil, true, nil
	}
	if length < 4 {
		return nil, false, fmt.Errorf("unsupported packet control %q", header)
	}
	payload := make([]byte, int(length)-4)
	if _, err := io.ReadFull(reader.reader, payload); err != nil {
		return nil, false, err
	}
	return payload, false, nil
}

func writeFilterResponse(output io.Writer, status string, content []byte) error {
	if err := writePacket(output, []byte("status="+status+"\n")); err != nil {
		return err
	}
	if err := writeFlush(output); err != nil {
		return err
	}
	for len(content) > 0 {
		length := min(len(content), maxPacketPayload)
		if err := writePacket(output, content[:length]); err != nil {
			return err
		}
		content = content[length:]
	}
	if err := writeFlush(output); err != nil {
		return err
	}
	return writeFlush(output)
}

func writePacket(output io.Writer, payload []byte) error {
	if len(payload) > maxPacketPayload {
		return errors.New("packet payload is too large")
	}
	_, err := fmt.Fprintf(output, "%04x%s", len(payload)+4, payload)
	return err
}

func writeFlush(output io.Writer) error {
	_, err := io.WriteString(output, "0000")
	return err
}
