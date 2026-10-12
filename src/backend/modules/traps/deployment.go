package traps

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"

	"honey-forge/internal/platform/httpx"
)

type DeploymentNode struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Address  string `json:"address"`
	Ready    bool   `json:"ready"`
}

func (r *Repository) DeploymentNodes(ctx context.Context, org string) ([]DeploymentNode, error) {
	if err := readAccess(ctx, org); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT id,hostname,address,ready AND observed_at > clock_timestamp()-interval '30 seconds' FROM deployment_nodes WHERE organization_id=$1 ORDER BY hostname,id`, org)
	if err != nil {
		return nil, fmt.Errorf("list deployment nodes: %w", err)
	}

	defer rows.Close()

	nodes := []DeploymentNode{}
	for rows.Next() {
		var n DeploymentNode
		if err := rows.Scan(&n.ID, &n.Hostname, &n.Address, &n.Ready); err != nil {
			return nil, fmt.Errorf("scan deployment node: %w", err)
		}

		nodes = append(nodes, n)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read deployment nodes: %w", err)
	}

	return nodes, nil
}

func (s *Service) DeploymentNodes(ctx context.Context) ([]DeploymentNode, error) {
	org, err := access(ctx, false)
	if err != nil {
		return nil, err
	}

	repo, ok := s.store.(interface {
		DeploymentNodes(context.Context, string) ([]DeploymentNode, error)
	})
	if !ok {
		return []DeploymentNode{}, nil
	}

	return repo.DeploymentNodes(ctx, org)
}

func (h *Handler) deploymentNodes(c *gin.Context) {
	if !noQuery(c) || !httpx.EmptyBody(c) {
		return
	}

	nodes, err := h.service.DeploymentNodes(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(200, gin.H{"items": nodes})
}
