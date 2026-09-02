package controllers

// Publish/Revert: syncing the draft roster to and from real Planning
// Center team members, plus the plan-picker status check.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// rosterStatus classifies a plan for the picker list - see the scheduling
// controller's planStatus, which this mirrors exactly (substituting
// team_members for items as the "does Planning Center already have content
// here" check).
func (sc *SchedulingController) rosterStatus(ctx context.Context, organizationID, serviceTypeID, planID string) string {
	if roster, found, err := sc.store.FindRoster(ctx, organizationID, serviceTypeID, planID); err == nil && found {
		assignments, assignErr := sc.store.ListRosterAssignments(ctx, roster.ID)
		pending, pendingErr := sc.store.ListPendingRosterDeletions(ctx, roster.ID)
		if assignErr == nil && pendingErr == nil && len(assignments) > 0 {
			for _, ra := range assignments {
				if ra.PCOPlanPersonID == "" {
					return "pending"
				}
			}
			if len(pending) > 0 {
				return "pending"
			}
			return "planned"
		}
	}

	members, err := pco.GetTeamMembers(ctx, serviceTypeID, planID, nil)
	if err != nil {
		return ""
	}
	if len(members.Data) > 0 {
		return "todo"
	}
	return ""
}

// PublishRoster fully syncs the draft roster to the real Planning Center
// plan - the people equivalent of PublishSet, but simpler: there's no
// reorder step at all, since position assignments have no sequence.
func (sc *SchedulingController) PublishRoster(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	roster, err := sc.store.GetOrCreateRoster(ctx, orgID, serviceTypeID, planID)
	if err != nil {
		return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, err.Error())))
	}

	assignments, err := sc.store.ListRosterAssignments(ctx, roster.ID)
	if err != nil {
		return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, err.Error())))
	}

	pendingDeletions, err := sc.store.ListPendingRosterDeletions(ctx, roster.ID)
	if err != nil {
		return render(c, views.RosterBuilderBody(sc.buildRosterBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, err.Error())))
	}

	// failures is appended from concurrent goroutines below (both the
	// deletion and creation fan-outs), guarded by failuresMu throughout -
	// a plain slice append isn't safe for concurrent writers on its own.
	// deleted/created are tallied via atomic counters for the same reason.
	// Each fan-out still fully completes (RunConcurrently blocks until
	// every goroutine returns) before the next one starts, so the
	// deletions-before-creates ordering the sequential version had is
	// unchanged - PublishRoster has no reorder step to worry about
	// afterward (see this function's own doc comment).
	var failures []string
	var failuresMu sync.Mutex
	var deletedCount, createdCount int32

	names := personNameMapForIDs(ctx, assignmentPersonIDs(append(append([]scheduling.RosterAssignment{}, assignments...), pendingDeletions...)))

	scheduling.RunConcurrently(pendingDeletions, func(ra scheduling.RosterAssignment) {
		name := titleOrID(names, ra.PersonID)

		if err := pco.DeleteTeamMember(ctx, serviceTypeID, planID, ra.PCOPlanPersonID); err != nil {
			failuresMu.Lock()
			failures = append(failures, fmt.Sprintf("removing %s: %s", name, friendlyPCOErr(err)))
			failuresMu.Unlock()
			return
		}
		if err := sc.store.DeleteAssignmentRow(ctx, ra.ID); err != nil {
			failuresMu.Lock()
			failures = append(failures, fmt.Sprintf("removed %s from Planning Center but failed to clear it locally: %s", name, err.Error()))
			failuresMu.Unlock()
			return
		}
		atomic.AddInt32(&deletedCount, 1)
	})

	var toCreate []scheduling.RosterAssignment
	for _, ra := range assignments {
		if ra.PCOPlanPersonID == "" {
			toCreate = append(toCreate, ra)
		}
	}

	scheduling.RunConcurrently(toCreate, func(ra scheduling.RosterAssignment) {
		name := titleOrID(names, ra.PersonID)

		member, err := pco.CreateTeamMember(ctx, serviceTypeID, planID, &pco.CreateTeamMemberParams{
			PersonID:         ra.PersonID,
			TeamID:           ra.TeamID,
			TeamPositionName: ra.TeamPositionName,
		})
		if err != nil {
			failuresMu.Lock()
			failures = append(failures, fmt.Sprintf("%s: %s", name, friendlyPCOErr(err)))
			failuresMu.Unlock()
			return
		}
		if err := sc.store.MarkAssignmentPublished(ctx, roster.ID, ra.ID, member.Data.ID); err != nil {
			failuresMu.Lock()
			failures = append(failures, fmt.Sprintf("%s: created in Planning Center but failed to record locally: %s", name, err.Error()))
			failuresMu.Unlock()
			return
		}
		atomic.AddInt32(&createdCount, 1)
	})

	deleted, created := int(deletedCount), int(createdCount)

	var summary []string
	if created > 0 {
		summary = append(summary, fmt.Sprintf("added %d", created))
	}
	if deleted > 0 {
		summary = append(summary, fmt.Sprintf("removed %d", deleted))
	}

	publishMsg := ""
	if len(summary) > 0 {
		publishMsg = "Synced to Planning Center: " + strings.Join(summary, ", ") + "."
	} else if len(failures) == 0 {
		publishMsg = "Nothing to publish - already in sync with Planning Center."
	}

	errMsg := ""
	if len(failures) > 0 {
		errMsg = "Failed to sync: " + strings.Join(failures, "; ")
	}

	data := sc.buildRosterBuilderData(ctx, orgID, serviceTypeID, planID)
	sc.triggerPersonDataSync(c)
	data.PublishMsg = publishMsg
	if errMsg != "" {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
	}

	return render(c, views.RosterBuilderBody(data))
}

// RevertRoster discards local changes and rebuilds the roster from whatever
// Planning Center's plan currently has - see sets.RevertSet, which this
// mirrors.
func (sc *SchedulingController) RevertRoster(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	roster, err := sc.store.GetOrCreateRoster(ctx, orgID, serviceTypeID, planID)
	errMsg := ""
	publishMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else {
		members, err := pco.GetTeamMembers(ctx, serviceTypeID, planID, nil)
		if err != nil {
			errMsg = "loading plan team members from Planning Center: " + err.Error()
		} else {
			importRows := make([]scheduling.RosterAssignment, 0, len(members.Data))
			for _, m := range members.Data {
				teamID := ""
				if m.Relationships.Team.Data != nil {
					teamID = m.Relationships.Team.Data.ID
				}
				personID := ""
				if m.Relationships.Person.Data != nil {
					personID = m.Relationships.Person.Data.ID
				}
				importRows = append(importRows, scheduling.RosterAssignment{
					PersonID:         personID,
					TeamID:           teamID,
					TeamPositionName: m.Attributes.TeamPositionName,
					PCOPlanPersonID:  m.ID,
					Status:           m.Attributes.Status,
				})
			}

			if err := sc.store.RevertRoster(ctx, roster.ID, importRows); err != nil {
				errMsg = err.Error()
			} else {
				publishMsg = "Reverted to match Planning Center."
			}
		}
	}

	data := sc.buildRosterBuilderData(ctx, orgID, serviceTypeID, planID)
	sc.triggerPersonDataSync(c)
	data.PublishMsg = publishMsg
	if errMsg != "" {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
	}

	return render(c, views.RosterBuilderBody(data))
}
