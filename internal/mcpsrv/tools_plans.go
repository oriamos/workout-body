package mcpsrv

import (
	"context"
	"encoding/json"

	"github.com/fullfran/training-mcp/internal/training"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const planPrescriptionRule = "A plan prescribes what is planned (sets, rep range, optional target load in kg, target RPE 1-10, target RIR 0-10); omitted targets mean 'not prescribed'. Plans never create completed sets."

type PlanItemInput struct {
	Exercise       string   `json:"exercise" jsonschema:"Exercise name or alias; normalized to lower case."`
	TargetSets     int      `json:"target_sets" jsonschema:"Planned number of sets, 1-20."`
	RepMin         int      `json:"rep_min,omitempty" jsonschema:"Lower bound of the target rep range, e.g. 10 for '10 to 12'."`
	RepMax         int      `json:"rep_max,omitempty" jsonschema:"Upper bound of the target rep range, e.g. 12 for '10 to 12'."`
	TargetRPE      *float64 `json:"target_rpe,omitempty" jsonschema:"Prescribed RPE 1-10. Omit if not prescribed."`
	TargetWeightKG *float64 `json:"target_weight_kg,omitempty" jsonschema:"Prescribed load in kilograms, 0-2000. Omit if not prescribed."`
	TargetRIR      *float64 `json:"target_rir,omitempty" jsonschema:"Prescribed reps in reserve 0-10. Omit if not prescribed."`
	Superset       string   `json:"superset,omitempty" jsonschema:"Label grouping exercises done back to back, e.g. 'A'. Omit for a standalone exercise."`
	Notes          string   `json:"notes,omitempty" jsonschema:"Free-text intent: cues, tempo, whether to push the last set."`
}

func (it PlanItemInput) toItem() training.PlanItem {
	out := training.PlanItem{
		Exercise: it.Exercise, TargetSets: it.TargetSets, RepMin: it.RepMin, RepMax: it.RepMax,
		TargetWeightKG: it.TargetWeightKG, TargetRIR: it.TargetRIR, Superset: it.Superset, Notes: it.Notes,
	}
	if it.TargetRPE != nil {
		out.TargetRPE = *it.TargetRPE
	}
	return out
}

type CreatePlanInput struct {
	Name  string          `json:"name" jsonschema:"Name of the plan, e.g. 'Upper A'."`
	Notes string          `json:"notes,omitempty" jsonschema:"Optional free-text notes about the plan."`
	Items []PlanItemInput `json:"items" jsonschema:"Ordered exercises; order is the order performed. Each exercise at most once."`
}
type PlanOut struct {
	Plan training.Plan `json:"plan"`
}
type PlansOut struct {
	Plans []training.Plan `json:"plans"`
}
type PlanInput struct {
	PlanID int64 `json:"plan_id" jsonschema:"Id of the plan."`
}
type DeletePlanInput struct {
	PlanID           int64 `json:"plan_id" jsonschema:"Id of the plan to delete."`
	ExpectedRevision *int  `json:"expected_revision,omitempty" jsonschema:"The plan's revision from your last read; rejected with a conflict if it changed since."`
}
type UpdatePlanInput struct {
	PlanID int64   `json:"plan_id" jsonschema:"Id of the plan to edit."`
	Name   *string `json:"name,omitempty" jsonschema:"New name. Omit to leave unchanged."`
	Notes  *string `json:"notes,omitempty" jsonschema:"New notes for the whole routine. Empty string clears them. Omit to leave unchanged."`
}
type PlanItemEditInput struct {
	PlanID int64 `json:"plan_id" jsonschema:"Id of the plan to edit."`
	PlanItemInput
}
type PlanItemRefInput struct {
	PlanID   int64  `json:"plan_id" jsonschema:"Id of the plan to edit."`
	Exercise string `json:"exercise" jsonschema:"Exercise currently in the plan (name or alias)."`
}
type PlanMoveInput struct {
	PlanID    int64  `json:"plan_id" jsonschema:"Id of the plan to edit."`
	Exercise  string `json:"exercise" jsonschema:"Exercise currently in the plan."`
	Direction string `json:"direction" jsonschema:"'up' or 'down'. Order is the order the workout is performed in."`
}
type PlanDeleteOut struct {
	PlanID  int64 `json:"plan_id"`
	Deleted bool  `json:"deleted"`
}
type ImportPlanInput struct {
	Plan       json.RawMessage `json:"plan"`
	OnConflict string          `json:"on_conflict,omitempty" jsonschema:"What to do when a plan with the same name (case-insensitive) exists: 'error' (default) rejects the import; 'replace' replaces that plan's notes and exercises in place, keeping its id."`
	DryRun     bool            `json:"dry_run,omitempty" jsonschema:"Validate and report what would happen without writing anything."`
}
type ReplacePlanInput struct {
	PlanID           int64           `json:"plan_id" jsonschema:"Id of the plan to replace."`
	Plan             json.RawMessage `json:"plan"`
	ExpectedRevision *int            `json:"expected_revision,omitempty" jsonschema:"The plan's revision from your last read (get_plan); rejected with a conflict if it changed since."`
}
type ImportPlanOut struct {
	Result training.PlanImportResult `json:"result"`
}
type ExportPlanOut struct {
	Plan training.PlanDocument `json:"plan"`
}

// planDocumentSchema describes the portable plan format (docs/plan-format,
// docs/schemas/workout-plan.v1.schema.json). Unknown fields are not rejected
// by the schema: the engine reports them with their JSON path instead, which
// is more actionable than a protocol error.
func planDocumentSchema() *jsonschema.Schema {
	// Ranges are stated in descriptions rather than enforced by the schema, so
	// a bad value reaches the engine, which reports every problem at once with
	// its JSON path, instead of failing the call with a protocol error.
	num := func(desc string) *jsonschema.Schema { return &jsonschema.Schema{Type: "number", Description: desc} }
	integer := func(desc string) *jsonschema.Schema { return &jsonschema.Schema{Type: "integer", Description: desc} }
	str := func(desc string) *jsonschema.Schema { return &jsonschema.Schema{Type: "string", Description: desc} }
	item := &jsonschema.Schema{
		Type:     "object",
		Required: []string{"exercise", "sets"},
		Properties: map[string]*jsonschema.Schema{
			"exercise":         str("Exercise name or alias. Unknown names create a new exercise."),
			"sets":             integer("Planned number of sets, 1-20."),
			"rep_min":          integer("Lower end of the rep range, 1-1000."),
			"rep_max":          integer("Upper end of the rep range, 1-1000; >= rep_min."),
			"target_weight_kg": num("Optional planned load in kilograms, 0-2000."),
			"target_rpe":       num("Optional target RPE, 1-10."),
			"target_rir":       num("Optional target reps in reserve, 0-10."),
			"superset":         str("Items sharing a label are performed back to back."),
			"notes":            str("Cues, tempo, intent."),
		},
		PropertyOrder: []string{"exercise", "sets", "rep_min", "rep_max", "target_weight_kg", "target_rpe", "target_rir", "superset", "notes"},
	}
	return &jsonschema.Schema{
		Type: "object",
		Description: `Plan document in the portable "workout-plan" version 1 format (the same object export_plan returns): ` +
			`{"format":"workout-plan","version":1,"name":"Upper A","notes":"...","items":[{"exercise":"bench press","sets":3,"rep_min":6,"rep_max":8,"target_weight_kg":60,"target_rpe":8}]}. ` +
			`Each exercise may appear once (use sets for volume); 1-40 items. Omit a field to mean 'not prescribed'.`,
		Required: []string{"format", "version", "name", "items"},
		Properties: map[string]*jsonschema.Schema{
			"format":  {Type: "string", Const: jsonschema.Ptr[any](training.PlanFormat)},
			"version": {Type: "integer", Const: jsonschema.Ptr[any](training.PlanVersion)},
			"name":    str("Plan name; import matches existing plans by name, case-insensitively."),
			"notes":   str("Notes about the whole plan."),
			"items":   {Type: "array", Items: item, Description: "Exercises in the order performed."},
		},
		PropertyOrder: []string{"format", "version", "name", "notes", "items"},
	}
}

func registerPlanTools(r *registry, service *training.Service) {
	planAfter := func(ctx context.Context, id int64, err error) (*mcp.CallToolResult, PlanOut, error) {
		if err != nil {
			return nil, PlanOut{}, toolError(err)
		}
		v, err := service.GetPlan(ctx, id)
		return nil, PlanOut{Plan: v}, toolError(err)
	}

	createSchema := inputSchema[CreatePlanInput]()
	setNonEmpty(createSchema, "name")
	addTool(r, &mcp.Tool{
		Name:        "create_plan",
		Description: "Create a reusable workout plan: an ordered list of exercises, each with a target set count and optional rep range, target load, RPE and RIR. " + planPrescriptionRule + " To create from a plan document use import_plan.",
		InputSchema: createSchema,
		Annotations: additive("Create plan", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in CreatePlanInput) (*mcp.CallToolResult, PlanOut, error) {
		plan := training.Plan{Name: in.Name, Notes: in.Notes}
		for _, it := range in.Items {
			plan.Items = append(plan.Items, it.toItem())
		}
		v, err := service.CreatePlan(ctx, plan)
		return nil, PlanOut{Plan: v}, toolError(err)
	})

	addTool(r, &mcp.Tool{
		Name:        "list_plans",
		Description: "List every saved plan with its revision and total planned set count.",
		Annotations: readOnly("List plans"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, PlansOut, error) {
		v, err := service.ListPlans(ctx)
		return nil, PlansOut{Plans: v}, toolError(err)
	})

	planSchema := inputSchema[PlanInput]()
	setPositiveID(planSchema, "plan_id")
	addTool(r, &mcp.Tool{
		Name:        "get_plan",
		Description: "Read one plan with its revision (pass it as expected_revision to replace_plan/delete_plan) and ordered exercises with target sets, rep ranges, target load (kg), RPE and RIR.",
		InputSchema: planSchema,
		Annotations: readOnly("Get plan"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in PlanInput) (*mcp.CallToolResult, PlanOut, error) {
		v, err := service.GetPlan(ctx, in.PlanID)
		return nil, PlanOut{Plan: v}, toolError(err)
	})

	updateSchema := inputSchema[UpdatePlanInput]()
	setPositiveID(updateSchema, "plan_id")
	updateSchema.MinProperties = jsonschema.Ptr(2)
	addTool(r, &mcp.Tool{
		Name:        "update_plan",
		Description: "Edit a plan's name or notes without touching its exercises.",
		InputSchema: updateSchema,
		Annotations: destructive("Update plan", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in UpdatePlanInput) (*mcp.CallToolResult, PlanOut, error) {
		return planAfter(ctx, in.PlanID, service.UpdatePlan(ctx, in.PlanID, training.PlanPatch{Name: in.Name, Notes: in.Notes}))
	})

	itemSchema := inputSchema[PlanItemEditInput]()
	setPositiveID(itemSchema, "plan_id")
	setNonEmpty(itemSchema, "exercise")
	addTool(r, &mcp.Tool{
		Name:        "set_plan_exercise",
		Description: "Add an exercise to a saved plan, or replace its prescription. Editing a plan never changes workouts already started from it. " + planPrescriptionRule,
		InputSchema: itemSchema,
		Annotations: destructive("Set plan exercise", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in PlanItemEditInput) (*mcp.CallToolResult, PlanOut, error) {
		return planAfter(ctx, in.PlanID, service.SetPlanItem(ctx, in.PlanID, in.PlanItemInput.toItem()))
	})

	refSchema := inputSchema[PlanItemRefInput]()
	setPositiveID(refSchema, "plan_id")
	setNonEmpty(refSchema, "exercise")
	addTool(r, &mcp.Tool{
		Name:        "remove_plan_exercise",
		Description: "Remove an exercise from a saved plan and close the gap in its order.",
		InputSchema: refSchema,
		Annotations: destructive("Remove plan exercise", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in PlanItemRefInput) (*mcp.CallToolResult, PlanOut, error) {
		return planAfter(ctx, in.PlanID, service.RemovePlanItem(ctx, in.PlanID, in.Exercise))
	})

	moveSchema := inputSchema[PlanMoveInput]()
	setPositiveID(moveSchema, "plan_id")
	setNonEmpty(moveSchema, "exercise")
	prop(moveSchema, "direction").Enum = []any{"up", "down"}
	addTool(r, &mcp.Tool{
		Name:        "move_plan_exercise",
		Description: "Move an exercise one place up or down within a plan. The order is the order the workout is performed in.",
		InputSchema: moveSchema,
		Annotations: destructive("Move plan exercise", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in PlanMoveInput) (*mcp.CallToolResult, PlanOut, error) {
		delta := 1
		if in.Direction == "up" {
			delta = -1
		}
		return planAfter(ctx, in.PlanID, service.MovePlanItem(ctx, in.PlanID, in.Exercise, delta))
	})

	deleteSchema := inputSchema[DeletePlanInput]()
	setPositiveID(deleteSchema, "plan_id")
	addTool(r, &mcp.Tool{
		Name:        "delete_plan",
		Description: "Permanently delete a plan. Irreversible: confirm with the user first. Workouts that followed it keep their copied prescription and plan name. " + revisionRule,
		InputSchema: deleteSchema,
		Annotations: destructive("Delete plan", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DeletePlanInput) (*mcp.CallToolResult, PlanDeleteOut, error) {
		err := service.DeletePlanChecked(ctx, in.PlanID, in.ExpectedRevision)
		return nil, PlanDeleteOut{PlanID: in.PlanID, Deleted: err == nil}, toolError(err)
	})

	importSchema := inputSchema[ImportPlanInput]()
	importSchema.Properties["plan"] = planDocumentSchema()
	prop(importSchema, "on_conflict").Enum = []any{training.OnConflictError, training.OnConflictReplace}
	addTool(r, &mcp.Tool{
		Name: "import_plan",
		Description: "Validate a plan document and store it atomically. Plans are matched by name (case-insensitive): a new name creates a plan; an existing name is rejected unless on_conflict='replace', which replaces that plan's notes and exercises in place (keeping its id). " +
			"Workouts already created from the plan are never changed. Every problem is reported with its JSON path (e.g. items[2].sets) and nothing is written. Use dry_run=true to preview. " +
			"new_exercises lists exercise names that would be/were created, with similar existing names. " + planPrescriptionRule,
		InputSchema: importSchema,
		Annotations: destructive("Import plan", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ImportPlanInput) (*mcp.CallToolResult, ImportPlanOut, error) {
		res, err := service.ImportPlan(ctx, in.Plan, in.OnConflict, in.DryRun)
		return nil, ImportPlanOut{Result: res}, toolError(err)
	})

	exportSchema := inputSchema[PlanInput]()
	setPositiveID(exportSchema, "plan_id")
	addTool(r, &mcp.Tool{
		Name:        "export_plan",
		Description: "Return a plan as a portable \"workout-plan\" version 1 document. Passing it unchanged to import_plan (or replace_plan) recreates the same prescriptions.",
		InputSchema: exportSchema,
		Annotations: readOnly("Export plan"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in PlanInput) (*mcp.CallToolResult, ExportPlanOut, error) {
		doc, err := service.ExportPlan(ctx, in.PlanID)
		return nil, ExportPlanOut{Plan: doc}, toolError(err)
	})

	replaceSchema := inputSchema[ReplacePlanInput]()
	setPositiveID(replaceSchema, "plan_id")
	replaceSchema.Properties["plan"] = planDocumentSchema()
	addTool(r, &mcp.Tool{
		Name:        "replace_plan",
		Description: "Replace an existing plan's name, notes and exercises from a plan document, atomically, keeping its id. Workouts already created from it are not changed. Typical flow: export_plan or get_plan, edit, replace_plan with expected_revision. " + revisionRule,
		InputSchema: replaceSchema,
		Annotations: destructive("Replace plan", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ReplacePlanInput) (*mcp.CallToolResult, PlanOut, error) {
		v, err := service.ReplacePlan(ctx, in.PlanID, in.Plan, in.ExpectedRevision)
		return nil, PlanOut{Plan: v}, toolError(err)
	})
}
