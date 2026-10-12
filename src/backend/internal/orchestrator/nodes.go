package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"honey-forge/internal/contract"
)

type swarmNode struct {
	ID   string
	Spec struct {
		Role         string
		Availability string
		Labels       map[string]string
	}
	Description struct{ Hostname string }
	Status      struct{ State, Addr string }
}

func decodeNodes(data []byte) ([]swarmNode, error) {
	var all []swarmNode
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("decode Swarm nodes: %w", err)
	}

	nodes := []swarmNode{}
	for _, node := range all {
		if node.Spec.Role != "worker" || !contract.ValidID(node.Spec.Labels["honeyforge.organization"]) {
			continue
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}

// SyncNodes publishes only explicitly assigned workers, without giving the API Docker access.
func (s PGStore) SyncNodes(ctx context.Context, docker DockerCLI) (result error) {
	output, err := docker.call(ctx, nil, "node", "ls", "--quiet")
	if err != nil {
		return fmt.Errorf("list Swarm nodes: %w", err)
	}

	ids := strings.Fields(string(output))
	if len(ids) == 0 {
		return fmt.Errorf("Swarm has no nodes")
	}

	output, err = docker.call(ctx, nil, append([]string{"node", "inspect"}, ids...)...)
	if err != nil {
		return fmt.Errorf("inspect Swarm nodes: %w", err)
	}

	nodes, err := decodeNodes(output)
	if err != nil {
		return err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin node refresh: %w", err)
	}

	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			result = errors.Join(result, fmt.Errorf("rollback node refresh: %w", err))
		}
	}()

	if _, err := tx.Exec(ctx, `UPDATE deployment_nodes SET ready=false`); err != nil {
		return fmt.Errorf("mark old nodes unavailable: %w", err)
	}

	for _, node := range nodes {
		ready := node.Status.State == "ready" && node.Spec.Availability == "active"
		_, err := tx.Exec(ctx, `INSERT INTO deployment_nodes(id,organization_id,hostname,address,ready,observed_at)
			SELECT $1,id,$3,$4,$5,clock_timestamp() FROM organizations WHERE id=$2
			ON CONFLICT(id) DO UPDATE SET organization_id=excluded.organization_id,hostname=excluded.hostname,address=excluded.address,ready=excluded.ready,observed_at=excluded.observed_at`,
			node.ID, node.Spec.Labels["honeyforge.organization"], node.Description.Hostname, node.Status.Addr, ready)
		if err != nil {
			return fmt.Errorf("refresh deployment node: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit node refresh: %w", err)
	}

	return nil
}
