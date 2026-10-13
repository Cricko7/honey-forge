package redistrap

import (
	"strings"
	"sync"
)

type observedCommand struct {
	args       []string
	count      int64
	truncated  bool
	err        error
	unsafeFIFO bool
}

type sessionObserver struct {
	mu           sync.Mutex
	emitter      *emitter
	service      string
	commands     []observedCommand
	responses    []observedResponse
	uncorrelated bool
}

func (o *sessionObserver) request(command observedCommand) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if command.err == nil && len(command.args) == 0 {
		return nil
	}

	if command.unsafeFIFO || streamingCommand(command.args) || len(o.commands) >= 128 {
		if err := o.flush(); err != nil {
			return err
		}
	}

	if o.uncorrelated {
		return emitCommand(o.emitter, o.service, command.args, command.count, command.truncated, command.err, "unknown")
	}

	o.commands = append(o.commands, command)
	return o.match()
}

func streamingCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}

	switch strings.ToUpper(args[0]) {
	case "SUBSCRIBE", "PSUBSCRIBE", "SSUBSCRIBE", "UNSUBSCRIBE", "PUNSUBSCRIBE", "SUNSUBSCRIBE", "MONITOR", "SYNC", "PSYNC":
		return true
	case "CLIENT":
		return len(args) > 1 && strings.EqualFold(args[1], "REPLY")
	}

	return false
}

func (o *sessionObserver) reply(response observedResponse) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.uncorrelated || response.kind == '>' || response.kind == '|' {
		return o.emitResponse(response)
	}

	if len(o.responses) >= 128 {
		if err := o.flush(); err != nil {
			return err
		}

		return o.emitResponse(response)
	}

	o.responses = append(o.responses, response)
	return o.match()
}

func (o *sessionObserver) match() error {
	for len(o.commands) > 0 && len(o.responses) > 0 {
		command, response := o.commands[0], o.responses[0]
		// Redis can suppress delayed AUTH errors when pipelined AUTH replaces them.
		if len(command.args) > 0 && strings.EqualFold(command.args[0], "AUTH") {
			validArity := len(command.args) == 2 || len(command.args) == 3
			arityError := strings.HasPrefix(response.wire, "-ERR wrong number of arguments for 'auth'")
			if validArity == arityError {
				return o.flush()
			}
		}

		o.commands[0], o.responses[0] = observedCommand{}, observedResponse{}
		o.commands, o.responses = o.commands[1:], o.responses[1:]
		if err := emitCommand(o.emitter, o.service, command.args, command.count, command.truncated, command.err, responseOutcome(response)); err != nil {
			return err
		}

		if err := o.emitResponse(response); err != nil {
			return err
		}
	}

	return nil
}

func responseOutcome(response observedResponse) string {
	if response.kind == '-' || response.kind == '!' {
		return "rejected"
	}

	if response.kind == 0 {
		return "unknown"
	}

	return "accepted"
}

func (o *sessionObserver) emitResponse(response observedResponse) error {
	input, cut := clip("Redis response: "+response.wire, 4096)
	return o.emitter.emit("service.action", map[string]any{
		"service": o.service, "action_kind": "request", "input": input, "outcome": responseOutcome(response),
		"received_bytes": min(response.count, 8192), "truncated": cut || response.count > int64(len(response.wire)),
	})
}

// Caller holds mu, or has already joined the response goroutine.
func (o *sessionObserver) flush() error {
	o.uncorrelated = true
	for _, command := range o.commands {
		if err := emitCommand(o.emitter, o.service, command.args, command.count, command.truncated, command.err, "unknown"); err != nil {
			return err
		}
	}

	o.commands = nil
	for _, response := range o.responses {
		if err := o.emitResponse(response); err != nil {
			return err
		}
	}

	o.responses = nil
	return nil
}
