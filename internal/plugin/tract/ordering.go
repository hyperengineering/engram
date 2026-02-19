package tract

import (
	"encoding/json"
	"sort"

	"github.com/hyperengineering/engram/internal/sync"
)

// tablePriority defines the FK hierarchy level for each table.
// Lower priority = closer to root = must be upserted first.
var tablePriority = map[string]int{
	"goals":                   0,
	"csfs":                    1,
	"fwus":                    2,
	"implementation_contexts": 3,
}

// reorderForFK reorders entries so that FK constraints are satisfied during replay.
//
// Algorithm:
// 1. Separate entries into deletes and upserts.
// 2. Order deletes: children first (highest priority first), then parents.
// 3. Order upserts: parents first (lowest priority first), then children.
// 4. Within the goals table upserts, do a topological sort on parent_goal_id
//    so that parent goals precede child goals.
// 5. Return: deletes (child-first) ++ upserts (parent-first).
func reorderForFK(entries []sync.ChangeLogEntry) []sync.ChangeLogEntry {
	if len(entries) == 0 {
		return entries
	}

	var deletes, upserts []sync.ChangeLogEntry

	for _, e := range entries {
		switch e.Operation {
		case sync.OperationDelete:
			deletes = append(deletes, e)
		default:
			upserts = append(upserts, e)
		}
	}

	// Sort deletes: highest table priority first (children before parents)
	sort.SliceStable(deletes, func(i, j int) bool {
		pi := tablePriority[deletes[i].TableName]
		pj := tablePriority[deletes[j].TableName]
		return pi > pj
	})

	// Sort upserts: lowest table priority first (parents before children)
	sort.SliceStable(upserts, func(i, j int) bool {
		pi := tablePriority[upserts[i].TableName]
		pj := tablePriority[upserts[j].TableName]
		return pi < pj
	})

	// Within goals upserts, topological sort by parent_goal_id
	upserts = sortGoalsByParentage(upserts)

	// Final order: deletes first, then upserts
	result := make([]sync.ChangeLogEntry, 0, len(entries))
	result = append(result, deletes...)
	result = append(result, upserts...)
	return result
}

// sortGoalsByParentage reorders goal upserts so that parent goals
// appear before child goals (based on parent_goal_id).
// Non-goal entries pass through unchanged in their current position.
func sortGoalsByParentage(entries []sync.ChangeLogEntry) []sync.ChangeLogEntry {
	// Extract goal upserts and their indices
	type goalInfo struct {
		index        int
		entry        sync.ChangeLogEntry
		parentGoalID string // "" if root
	}

	var goals []goalInfo
	goalIndices := make(map[int]bool)

	for i, e := range entries {
		if e.TableName == "goals" && e.Operation == sync.OperationUpsert {
			parentID := extractParentGoalID(e.Payload)
			goals = append(goals, goalInfo{
				index:        i,
				entry:        e,
				parentGoalID: parentID,
			})
			goalIndices[i] = true
		}
	}

	if len(goals) <= 1 {
		return entries // Nothing to sort
	}

	// Build adjacency: goalID -> []children
	goalIDs := make(map[string]int) // entityID -> index in goals slice
	for i, g := range goals {
		goalIDs[g.entry.EntityID] = i
	}

	// Kahn's algorithm for topological sort
	inDegree := make(map[string]int)
	children := make(map[string][]string) // parentID -> childIDs

	for _, g := range goals {
		id := g.entry.EntityID
		if _, exists := inDegree[id]; !exists {
			inDegree[id] = 0
		}
		if g.parentGoalID != "" {
			// Only count in-degree for parents that are in this batch
			if _, parentInBatch := goalIDs[g.parentGoalID]; parentInBatch {
				inDegree[id]++
				children[g.parentGoalID] = append(children[g.parentGoalID], id)
			}
		}
	}

	// Start with roots (in-degree 0)
	var queue []string
	for _, g := range goals {
		if inDegree[g.entry.EntityID] == 0 {
			queue = append(queue, g.entry.EntityID)
		}
	}

	var sortedGoals []sync.ChangeLogEntry
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]

		idx := goalIDs[id]
		sortedGoals = append(sortedGoals, goals[idx].entry)

		for _, childID := range children[id] {
			inDegree[childID]--
			if inDegree[childID] == 0 {
				queue = append(queue, childID)
			}
		}
	}

	// If there are goals not in the sorted output (cycle or orphans with
	// parents not in this batch), append them at the end
	sortedIDs := make(map[string]bool)
	for _, g := range sortedGoals {
		sortedIDs[g.EntityID] = true
	}
	for _, g := range goals {
		if !sortedIDs[g.entry.EntityID] {
			sortedGoals = append(sortedGoals, g.entry)
		}
	}

	// Rebuild the entries slice, replacing goal entries with sorted versions
	result := make([]sync.ChangeLogEntry, 0, len(entries))
	goalIdx := 0
	for i, e := range entries {
		if goalIndices[i] {
			result = append(result, sortedGoals[goalIdx])
			goalIdx++
		} else {
			result = append(result, e)
		}
	}

	return result
}

// extractParentGoalID reads parent_goal_id from a goal's JSON payload.
// Returns "" if the field is null, missing, or the payload is not parseable.
func extractParentGoalID(payload json.RawMessage) string {
	if payload == nil {
		return ""
	}
	var data struct {
		ParentGoalID *string `json:"parent_goal_id"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		return ""
	}
	if data.ParentGoalID == nil {
		return ""
	}
	return *data.ParentGoalID
}
