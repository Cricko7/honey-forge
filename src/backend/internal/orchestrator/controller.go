package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"honey-forge/internal/decoys"
	"honey-forge/modules/profiles"
)

// ErrTrapReconciliation means the cycle visited all targets but some traps failed.
var ErrTrapReconciliation = errors.New("individual trap reconciliation failed")

type Target struct {
	DeploymentNodeID       string
	ID                     string
	OrganizationID         string
	ActorID                string
	Generation             int64
	Snapshot               profiles.Snapshot
	Deleted                bool
	AppliedProfileRevision *int32
	ActiveCommandID        *string
	HasStarted             bool
}

type ServiceState struct {
	DeploymentNodeID   string
	CertificateExpires time.Time
	UnsafeNetwork      bool
	Exists             bool
	Generation         int64
	Ports              []int
}

type Store interface {
	List(context.Context) ([]Target, error)
	Issue(context.Context, Target) (string, int64, error)
	Advance(context.Context, Target) error
}

type Docker interface {
	States(context.Context, []string) (map[string]ServiceState, error)
	Remove(context.Context, string) error
	Deploy(context.Context, Target, string, int64, []int) error
}

type Controller struct {
	Store  Store
	Docker Docker
	Ports  func(context.Context, profiles.Snapshot) ([]int, error)
}

func (c Controller) Reconcile(ctx context.Context) error {
	targets, err := c.Store.List(ctx)
	if err != nil {
		return fmt.Errorf("list managed traps: %w", err)
	}

	ids := make([]string, len(targets))
	for i, target := range targets {
		ids[i] = target.ID
	}

	states, err := c.Docker.States(ctx, ids)
	if err != nil {
		return fmt.Errorf("inspect managed traps: %w", err)
	}

	portsFor := c.Ports
	if portsFor == nil {
		portsFor = decoys.Ports
	}

	var failures []error
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}

		state := states[target.ID]
		if target.Deleted {
			if err := c.Docker.Remove(ctx, target.ID); err != nil {
				failures = append(failures, fmt.Errorf("remove trap %s: %w", target.ID, err))
			}

			continue
		}

		ports, err := portsFor(ctx, target.Snapshot)
		if err != nil {
			failures = append(failures, fmt.Errorf("ports for trap %s: %w", target.ID, err))
			continue
		}

		slices.Sort(ports)
		certificateCurrent := state.CertificateExpires.IsZero() || state.CertificateExpires.After(time.Now().Add(24*time.Hour))
		current := state.Exists && !state.UnsafeNetwork && state.Generation == target.Generation && state.DeploymentNodeID == target.DeploymentNodeID && slices.Equal(state.Ports, ports) && certificateCurrent
		if state.Exists && !current {
			if err := c.Docker.Remove(ctx, target.ID); err != nil {
				failures = append(failures, fmt.Errorf("replace trap %s: %w", target.ID, err))
				continue
			}
		}

		if !current {
			token, generation, err := c.Store.Issue(ctx, target)
			if err != nil {
				failures = append(failures, fmt.Errorf("issue trap %s credentials: %w", target.ID, err))
				continue
			}

			if err := c.Docker.Deploy(ctx, target, token, generation, ports); err != nil {
				failures = append(failures, fmt.Errorf("deploy trap %s: %w", target.ID, err))
				continue
			}
		}

		if err := c.Store.Advance(ctx, target); err != nil {
			failures = append(failures, fmt.Errorf("start trap %s: %w", target.ID, err))
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("%w: %w", ErrTrapReconciliation, errors.Join(failures...))
	}

	return nil
}
