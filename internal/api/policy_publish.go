package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"vpn-probe/internal/model"
	"vpn-probe/internal/monitor"
	"vpn-probe/internal/publish"
)

func (s *APIServer) initPublisher() {
	path := os.Getenv("VPN_PROBE_PUBLISH_JOBS")
	if path == "" {
		path = filepath.Join("data", "publish_jobs.json")
	}
	mgr, err := publish.NewManager(path, func(ctx context.Context, step func(string)) error {
		return s.applyPolicyGroups(ctx, step)
	})
	if err != nil {
		s.publishInitError = err
		return
	}
	s.policyPublisher = mgr
}

func (s *APIServer) applyPolicyGroups(ctx context.Context, step func(string)) error {
	if s.orchestrator == nil || s.substoreClient == nil {
		return fmt.Errorf("orchestrator/substore unavailable")
	}
	groups, err := monitor.LoadCustomGroups()
	if err != nil {
		return err
	}
	atoms := s.orchestrator.CollectAtoms()

	// Differentiated scripts: ashan gets all groups, Q strictly gets backbone-only groups
	scriptAshan, err := publish.CompileScript(groups, atoms, "ashan")
	if err != nil {
		return err
	}
	scriptQ, err := publish.CompileScript(groups, atoms, "q")
	if err != nil {
		return err
	}

	expected := []string{}
	required := map[string][]string{}
	for _, g := range groups {
		if g.Enabled {
			expected = append(expected, g.DisplayName)
			matched := s.orchestrator.EvaluateGroupAtoms(g.Filter)
			for _, a := range matched {
				required[g.DisplayName] = append(required[g.DisplayName], a.Name)
			}
		}
	}

	err = (publish.Publisher{Client: s.substoreClient}).ApplyDifferentiated(
		ctx,
		scriptAshan,
		scriptQ,
		func(fileName string, yaml []byte) error {
			isQ := strings.EqualFold(fileName, "q")
			return publish.ValidateYAMLWithMembers(yaml, expected, required, isQ)
		},
		step,
	)
	if err != nil {
		return err
	}

	if s.snapshotMgr != nil {
		s.snapshotMgr.TriggerAsyncWarmup([]string{"Q", "q", "ashan", "Ashan"})
	}
	return nil
}

func (s *APIServer) enqueuePolicyPublish(c *fiber.Ctx, accepted bool) error {
	if s.policyPublisher == nil {
		msg := "publisher unavailable"
		if s.publishInitError != nil {
			msg = s.publishInitError.Error()
		}
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": msg})
	}
	job, err := s.policyPublisher.Enqueue()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if accepted {
		c.Status(fiber.StatusAccepted)
	}
	return c.JSON(fiber.Map{
		"status":   "queued",
		"job_id":   job.ID,
		"revision": job.Revision,
		"message":  "configuration saved; publication queued",
	})
}

func (s *APIServer) handlePolicyGroupList(c *fiber.Ctx) error {
	return s.handleGetCustomGroups(c)
}

func (s *APIServer) handlePolicyGroupDefaults(c *fiber.Ctx) error {
	defaults := model.DefaultCustomGroups()
	return c.JSON(fiber.Map{
		"status": "ok",
		"count":  len(defaults),
		"groups": defaults,
	})
}

func (s *APIServer) handlePolicyGroupBatchSave(c *fiber.Ctx) error {
	var payload struct {
		Groups []model.CustomGroupDef `json:"groups"`
	}
	if err := c.BodyParser(&payload); err != nil || len(payload.Groups) == 0 {
		var rawGroups []model.CustomGroupDef
		if err2 := c.BodyParser(&rawGroups); err2 == nil && len(rawGroups) > 0 {
			payload.Groups = rawGroups
		} else if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
	}
	if len(payload.Groups) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "empty or invalid policy groups batch payload"})
	}
	if err := monitor.SaveCustomGroupsBatch(payload.Groups); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return s.enqueuePolicyPublish(c, true)
}

func (s *APIServer) handlePolicyGroupUpsert(c *fiber.Ctx) error {
	var def model.CustomGroupDef
	if err := c.BodyParser(&def); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	id := c.Params("id")
	if id != "" {
		if def.Name != "" && def.Name != id {
			return c.Status(400).JSON(fiber.Map{"error": "ID is immutable"})
		}
		def.Name = id
		groups, err := monitor.LoadCustomGroups()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		exists := false
		for _, g := range groups {
			if g.Name == id {
				exists = true
				break
			}
		}
		if !exists {
			return c.Status(404).JSON(fiber.Map{"error": "group not found"})
		}
	}
	if err := def.Validate(); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if err := monitor.SaveCustomGroup(def); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return s.enqueuePolicyPublish(c, id == "")
}

func (s *APIServer) handlePolicyGroupDelete(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return c.Status(400).JSON(fiber.Map{"error": "missing ID"})
	}
	if err := monitor.DeleteCustomGroup(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return s.enqueuePolicyPublish(c, false)
}

func (s *APIServer) handlePolicyGroupPreview(c *fiber.Ctx) error {
	var def model.CustomGroupDef
	if err := c.BodyParser(&def); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if s.orchestrator == nil {
		return c.Status(503).JSON(fiber.Map{"error": "orchestrator unavailable"})
	}
	atoms := s.orchestrator.CollectAtoms()
	matched := monitor.MatchAtoms(atoms, def.Filter)
	nodes := make([]fiber.Map, 0, len(matched))
	for _, a := range matched {
		nodes = append(nodes, fiber.Map{
			"id":         a.ID,
			"name":       a.Name,
			"country":    a.Country,
			"kind":       a.Kind,
			"infra_type": a.InfraType,
			"tags":       a.Tags,
			"online":     a.Online,
		})
	}
	return c.JSON(fiber.Map{"status": "ok", "count": len(nodes), "nodes": nodes})
}

func (s *APIServer) handleUpdateAtomMetadata(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return c.Status(400).JSON(fiber.Map{"error": "missing atom ID"})
	}
	var meta model.NodeMetadata
	if err := c.BodyParser(&meta); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if err := monitor.SaveNodeMetadata(id, meta); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return s.enqueuePolicyPublish(c, false)
}

func (s *APIServer) handleGetLatestPublishJob(c *fiber.Ctx) error {
	if s.policyPublisher == nil {
		return c.Status(503).JSON(fiber.Map{"error": "publisher unavailable"})
	}
	j, ok := s.policyPublisher.Latest()
	if !ok {
		return c.JSON(fiber.Map{"status": "never_published"})
	}
	return c.JSON(j)
}

func (s *APIServer) handleGetPublishJob(c *fiber.Ctx) error {
	if s.policyPublisher == nil {
		return c.Status(503).JSON(fiber.Map{"error": "publisher unavailable"})
	}
	id := c.Params("id")
	j, ok := s.policyPublisher.Get(id)
	if !ok {
		return c.Status(404).JSON(fiber.Map{"error": "job not found"})
	}
	return c.JSON(j)
}

func (s *APIServer) handleRetryPublishJob(c *fiber.Ctx) error {
	if s.policyPublisher == nil {
		return c.Status(503).JSON(fiber.Map{"error": "publisher unavailable"})
	}
	id := c.Params("id")
	j, err := s.policyPublisher.Retry(id)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "queued", "job_id": j.ID, "revision": j.Revision})
}
