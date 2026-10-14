package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"honey-forge/internal/decoys"
	"honey-forge/internal/platform/pipeline"
	"honey-forge/internal/platform/traffic"
	"honey-forge/modules/profiles"
)

type childBootstrap struct {
	URL         string            `json:"url"`
	Token       string            `json:"token"`
	Certificate []byte            `json:"certificate"`
	Snapshot    profiles.Snapshot `json:"snapshot"`
}

func readChildBootstrap(reader io.Reader) (childBootstrap, error) {
	return readBootstrapLine(bufio.NewReaderSize(reader, (1<<20)+1))
}

func readBootstrapLine(reader *bufio.Reader) (childBootstrap, error) {
	var bootstrap childBootstrap
	line, err := reader.ReadSlice('\n')
	if err != nil && err != io.EOF || len(line) > 1<<20 {
		return bootstrap, fmt.Errorf("trap bootstrap exceeds size limit or is incomplete")
	}

	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bootstrap); err != nil {
		return bootstrap, fmt.Errorf("read trap bootstrap: %w", err)
	}

	if err := decoder.Decode(new(any)); err != io.EOF {
		return bootstrap, fmt.Errorf("trap bootstrap must contain one JSON object")
	}

	if bootstrap.URL == "" || bootstrap.Token == "" || len(bootstrap.Certificate) == 0 {
		return bootstrap, fmt.Errorf("incomplete trap bootstrap")
	}

	return bootstrap, nil
}

func RunTrap(ctx context.Context) error {
	if os.Getenv("NM_STDIO") == "1" {
		stop := handleStdioPipeSignals()
		defer stop() // Keep SIGPIPE handling active through runtime and TLS cleanup.
	}

	reader := bufio.NewReaderSize(os.Stdin, (1<<20)+1)
	bootstrap, err := readBootstrapLine(reader)
	if err != nil {
		return err
	}

	if role := os.Getenv("NM_ROLE"); role != "" {
		allowed, err := decoys.ForType(role)
		if err != nil {
			return fmt.Errorf("image capabilities: %w", err)
		}
		matches := false
		for _, capability := range allowed {
			if capability.TypeID == bootstrap.Snapshot.TypeID && int32(capability.TypeVersion) == bootstrap.Snapshot.TypeVersion {
				matches = true
				break
			}
		}
		if !matches {
			return fmt.Errorf("snapshot type does not match image")
		}
	}

	start, err := decoys.Select(bootstrap.Snapshot)
	if err != nil {
		return err
	}

	var client *LocalClient
	if os.Getenv("NM_STDIO") == "1" {
		client, err = dialLocalPipe(ctx, bootstrap.URL, bootstrap.Token, bootstrap.Certificate, &stdioConn{reader: reader, input: os.Stdin, output: os.Stdout})
	} else {
		client, err = DialLocal(ctx, bootstrap.URL, bootstrap.Token, bootstrap.Certificate)
	}

	if err != nil {
		return err
	}

	defer client.Close()

	runtime, err := start(pipeline.WithSender(traffic.WithSink(ctx, client.Mirror), client.SendPipeline), bootstrap.Snapshot, "", client.Emit)
	if err != nil {
		return err
	}

	defer runtime.Stop()

	if err := client.Ready(ctx); err != nil {
		return err
	}

	// Parent pipe EOF also stops an orphaned trap when the agent exits or crashes.
	done := make(chan struct{})
	if os.Getenv("NM_STDIO") != "1" {
		go func() {
			if _, err := io.Copy(io.Discard, reader); err != nil {
				slog.DebugContext(ctx, "trap shutdown pipe closed", "error", err)
			}

			close(done)
		}()
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-done:
			return nil
		case <-client.Done():
			return fmt.Errorf("local agent connection closed")
		case delivery := <-client.PipelineIncoming:
			if receiver, ok := runtime.(interface {
				Receive(context.Context, pipeline.Delivery) error
			}); ok {
				if err := receiver.Receive(ctx, delivery); err != nil {
					return fmt.Errorf("pipeline delivery: %w", err)
				}
			}
		case <-ticker.C:
			if err := runtime.Err(); err != nil {
				return fmt.Errorf("trap runtime failed: %w", err)
			}
		}
	}
}
