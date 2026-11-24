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
	groupsAshan, err := monitor.LoadCustomGroups("ashan")
	if err != nil {
		return err
	}
	groupsQ, err := monitor.LoadCustomGroups("q")
	if err != nil {
		return err
	}
	groupsBusiness, err := monitor.LoadCustomGroups("business")
	if err != nil {
		return err
	}
	atoms := s.orchestrator.CollectAtoms()

	// Differentiated scripts: ashan (all), Q (strictly backbone), business (pure self-built, no airports)
	scriptAshan, err := publish.CompileScript(groupsAshan, atoms, "ashan")
	if err != nil {
		return err
	}
	scriptQ, err := publish.CompileScript(groupsQ, atoms, "q")
	if err != nil {
		return err
	}
	scriptBusiness, err := publish.CompileScript(groupsBusiness, atoms, "business")
	if err != nil {
		return err
	}

	expectedAshan := []string{}
	requiredAshan := map[string][]string{}
	for _, g := range groupsAshan {
		if g.Enabled {
			expectedAshan = append(expectedAshan, g.DisplayName)
			matched := s.orchestrator.EvaluateGroupAtoms(g.Filter)
			for _, a := range matched {
				requiredAshan[g.DisplayName] = append(requiredAshan[g.DisplayName], a.Name)
			}
		}
	}

	expectedQ := []string{}
	requiredQ := map[string][]string{}
	for _, g := range groupsQ {
		if g.Enabled {
			expectedQ = append(expectedQ, g.DisplayName)
			matched := s.orchestrator.EvaluateGroupAtoms(g.Filter)
			for _, a := range matched {
				if a.Kind == "transit" && !strings.Contains(a.Name, "家宽") && !strings.Contains(strings.ToLower(a.Name), "resi") {
					requiredQ[g.DisplayName] = append(requiredQ[g.DisplayName], a.Name)
				}
			}
		}
	}

	expectedBusiness := []string{}
	requiredBusiness := map[string][]string{}
	for _, g := range groupsBusiness {
		if g.Enabled {
			expectedBusiness = append(expectedBusiness, g.DisplayName)
			matched := s.orchestrator.EvaluateGroupAtoms(g.Filter)
			for _, a := range matched {
				requiredBusiness[g.DisplayName] = append(requiredBusiness[g.DisplayName], a.Name)
			}
		}
	}

	err = (publish.Publisher{Client: s.substoreClient}).ApplyDifferentiated(
		ctx,
		scriptAshan,
		scriptQ,
		scriptBusiness,
		func(fileName string, yaml []byte) error {
			if strings.EqualFold(fileName, "q") {
				return publish.ValidateYAMLWithProfile(yaml, expectedQ, requiredQ, "q")
			}
			if strings.EqualFold(fileName, "business") {
				return publish.ValidateYAMLWithProfile(yaml, expectedBusiness, requiredBusiness, "business")
			}
			return publish.ValidateYAMLWithProfile(yaml, expectedAshan, requiredAshan, "ashan")
		},
		step,
	)
	if err != nil {
		return err
	}

	if s.snapshotMgr != nil {
		s.snapshotMgr.TriggerAsyncWarmup([]string{"Q", "q", "ashan", "Ashan", "business", "Business"})
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
	profile := c.Query("profile", "ashan")
	defaults := model.DefaultCustomGroups(profile)
	return c.JSON(fiber.Map{
		"status":  "ok",
		"profile": profile,
		"count":   len(defaults),
		"groups":  defaults,
	})
}

func (s *APIServer) handlePolicyGroupBatchSave(c *fiber.Ctx) error {
	profile := c.Query("profile", "ashan")
	var payload struct {
		Profile string                 `json:"profile,omitempty"`
		Groups  []model.CustomGroupDef `json:"groups"`
	}
	if err := c.BodyParser(&payload); err != nil || len(payload.Groups) == 0 {
		var rawGroups []model.CustomGroupDef
		if err2 := c.BodyParser(&rawGroups); err2 == nil && len(rawGroups) > 0 {
			payload.Groups = rawGroups
		} else if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
	}
	if payload.Profile != "" {
		profile = payload.Profile
	}
	if len(payload.Groups) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "empty or invalid policy groups batch payload"})
	}
	if err := monitor.SaveCustomGroupsBatch(payload.Groups, profile); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return s.enqueuePolicyPublish(c, true)
}

func (s *APIServer) handlePolicyGroupUpsert(c *fiber.Ctx) error {
	profile := c.Query("profile", "ashan")
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
		groups, err := monitor.LoadCustomGroups(profile)
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
	if err := monitor.SaveCustomGroup(def, profile); err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "required") {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return s.enqueuePolicyPublish(c, id == "")
}

func (s *APIServer) handlePolicyGroupDelete(c *fiber.Ctx) error {
	profile := c.Query("profile", "ashan")
	id := c.Params("id")
	if id == "" {
		return c.Status(400).JSON(fiber.Map{"error": "missing ID"})
	}
	if err := monitor.DeleteCustomGroup(id, profile); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return s.enqueuePolicyPublish(c, false)
}

func (s *APIServer) handlePolicyGroupPreview(c *fiber.Ctx) error {
	profile := c.Query("profile", "ashan")
	var def model.CustomGroupDef
	if err := c.BodyParser(&def); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if s.orchestrator == nil {
		return c.Status(503).JSON(fiber.Map{"error": "orchestrator unavailable"})
	}
	atoms := s.orchestrator.CollectAtoms()
	isQ := strings.EqualFold(profile, "q")
	filter := def.Filter
	if isQ {
		filter.IncludeResidential = false
		filter.IncludeChain = false
		if len(filter.Kinds) > 0 {
			cleanKinds := []string{}
			for _, k := range filter.Kinds {
				if k == "transit" {
					cleanKinds = append(cleanKinds, k)
				}
			}
			filter.Kinds = cleanKinds
		}
	}
	matched := monitor.MatchAtoms(atoms, filter)
	if isQ {
		clean := make([]model.AtomNode, 0, len(matched))
		for _, a := range matched {
			if a.Kind == "transit" && !strings.Contains(a.Name, "家宽") && !strings.Contains(strings.ToLower(a.Name), "resi") {
				clean = append(clean, a)
			}
		}
		matched = clean
	}
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
	return c.JSON(fiber.Map{"status": "ok", "profile": profile, "count": len(nodes), "nodes": nodes})
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
