package mcpsrv

import (
	"context"
	"strconv"

	"github.com/fullfran/training-mcp/internal/training"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	nutritionUnits     = "Units: calories_kcal in kilocalories (kcal); protein_g, carbs_g and fat_g in grams. Values describe the WHOLE portion eaten, not per 100 g."
	nutritionMissing   = "Omit a value that is unknown: omitted means 'not recorded' and is never treated as 0; send 0 only for an explicit zero. Calories are stored exactly as given and never recomputed from macros."
	nutritionDates     = "Dates are local calendar dates YYYY-MM-DD in the configured timezone (default Asia/Jerusalem); omit date for today. Food cannot be logged for a future date. Nutrition never creates or changes a workout."
	nutritionIdem      = "idempotency_key (optional, at most 128 characters, global to this single user): retrying with the same key and the same payload replays the stored entry (replayed=true) without writing again; the same key with a different payload is a conflict. A meal eaten twice on purpose needs no key or a new key."
	nutritionItemsRule = "items (optional): when the user describes a meal with several components (e.g. 'yogurt with granola and blueberries'), prefer logging it as items - one per ingredient, each with its own name, amount_g (grams used, if known) and values for THAT amount (not per 100 g). Omit an unknown item value, never send 0 for unknown. The meal's calories_kcal/protein_g/carbs_g/fat_g are then computed as the sums of the items, so do not also give them on the meal; incomplete_values in the result names any value only some items provide (that sum is partial). Check suggest_nutrition_items first for ingredients the user logged before and reuse their values."
	targetsRule        = "Targets are only ever what the user explicitly asks for: never invent, suggest-and-save or recommend targets."
)

var nutrientClearEnum = []any{"calories_kcal", "protein_g", "carbs_g", "fat_g"}

type NutrientInput struct {
	CaloriesKcal *float64 `json:"calories_kcal,omitempty" jsonschema:"Energy of the whole portion in kcal, 0-20000. Omit if unknown."`
	ProteinG     *float64 `json:"protein_g,omitempty" jsonschema:"Protein of the whole portion in grams, 0-2000. Omit if unknown."`
	CarbsG       *float64 `json:"carbs_g,omitempty" jsonschema:"Carbohydrate of the whole portion in grams, 0-2000. Omit if unknown."`
	FatG         *float64 `json:"fat_g,omitempty" jsonschema:"Fat of the whole portion in grams, 0-2000. Omit if unknown."`
}

func (n NutrientInput) nutrients() training.Nutrients {
	return training.Nutrients{CaloriesKcal: n.CaloriesKcal, ProteinG: n.ProteinG, CarbsG: n.CarbsG, FatG: n.FatG}
}

// applyClear maps clear names onto the four Clear* flags.
func applyClear(names []string, cal, pro, carb, fat *bool) error {
	for i, c := range names {
		switch c {
		case "calories_kcal":
			*cal = true
		case "protein_g":
			*pro = true
		case "carbs_g":
			*carb = true
		case "fat_g":
			*fat = true
		default:
			return invalidArg("clear["+strconv.Itoa(i)+"]", "must be one of calories_kcal, protein_g, carbs_g, fat_g")
		}
	}
	return nil
}

// NutritionItemInput is one ingredient of a meal.
type NutritionItemInput struct {
	Name         string   `json:"name" jsonschema:"The ingredient, e.g. 'Greek yogurt 2%'. 1-200 characters."`
	AmountG      *float64 `json:"amount_g,omitempty" jsonschema:"Grams of this ingredient used, more than 0 and at most 5000. Omit if unknown."`
	CaloriesKcal *float64 `json:"calories_kcal,omitempty" jsonschema:"Energy of the amount used in kcal, 0-20000. Omit if unknown (never 0 for unknown)."`
	ProteinG     *float64 `json:"protein_g,omitempty" jsonschema:"Protein of the amount used in grams, 0-2000. Omit if unknown."`
	CarbsG       *float64 `json:"carbs_g,omitempty" jsonschema:"Carbohydrate of the amount used in grams, 0-2000. Omit if unknown."`
	FatG         *float64 `json:"fat_g,omitempty" jsonschema:"Fat of the amount used in grams, 0-2000. Omit if unknown."`
}

func nutritionItems(in []NutritionItemInput) []training.NutritionItem {
	if in == nil {
		return nil
	}
	out := make([]training.NutritionItem, 0, len(in))
	for _, it := range in {
		out = append(out, training.NutritionItem{Name: it.Name, AmountG: it.AmountG,
			Nutrients: training.Nutrients{CaloriesKcal: it.CaloriesKcal, ProteinG: it.ProteinG, CarbsG: it.CarbsG, FatG: it.FatG}})
	}
	return out
}

type AddNutritionInput struct {
	Name string `json:"name" jsonschema:"What was eaten, e.g. 'chicken and rice, large plate'. 1-200 characters."`
	NutrientInput
	Items          []NutritionItemInput `json:"items,omitempty" jsonschema:"Ingredients of the meal (at most 50), each with values for the amount used. When given, do not give calories_kcal/protein_g/carbs_g/fat_g on the meal: they are computed as the items' sums."`
	Date           string               `json:"date,omitempty" jsonschema:"Local date YYYY-MM-DD it was eaten. Omit for today."`
	IdempotencyKey string               `json:"idempotency_key,omitempty" jsonschema:"Optional retry key. Same key + same payload replays; different payload is a conflict."`
}
type NutritionEntryOut struct {
	Entry    training.NutritionEntry `json:"entry"`
	Replayed bool                    `json:"replayed"`
}
type EntryIDInput struct {
	EntryID int64 `json:"entry_id" jsonschema:"Id of the nutrition entry."`
}
type ListNutritionInput struct {
	Date  string `json:"date,omitempty" jsonschema:"Only entries of this local date YYYY-MM-DD. Do not combine with from/to."`
	From  string `json:"from,omitempty" jsonschema:"Inclusive earliest local date YYYY-MM-DD."`
	To    string `json:"to,omitempty" jsonschema:"Inclusive latest local date YYYY-MM-DD."`
	Query string `json:"query,omitempty" jsonschema:"Case-insensitive text to match in the entry name, e.g. 'oat'."`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum entries; defaults to 100, at most 500."`
}
type NutritionEntriesOut struct {
	Entries []training.NutritionEntry `json:"entries"`
}
type UpdateNutritionInput struct {
	EntryID int64   `json:"entry_id" jsonschema:"Id of the entry to change (from get_nutrition_day or list_nutrition_entries)."`
	Name    *string `json:"name,omitempty" jsonschema:"New name."`
	Date    *string `json:"date,omitempty" jsonschema:"Move the entry to this local date YYYY-MM-DD (not in the future)."`
	NutrientInput
	Clear            []string              `json:"clear,omitempty" jsonschema:"Values to reset to 'not recorded': any of calories_kcal, protein_g, carbs_g, fat_g."`
	Items            *[]NutritionItemInput `json:"items,omitempty" jsonschema:"Replaces the WHOLE ingredient list (send every item to keep, not only the changed one). A non-empty list recomputes the meal's values from the items (do not also send values or clear). An empty list [] removes the items and keeps the current sums as the entry's own values, which can then be edited directly."`
	ExpectedRevision *int                  `json:"expected_revision,omitempty" jsonschema:"The entry's revision from your last read; rejected with a conflict if it changed since."`
}

type SuggestItemsInput struct {
	Query string `json:"query,omitempty" jsonschema:"Case-insensitive text to match in the ingredient name, e.g. 'yogurt'. Omit for the most recent ingredients."`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum ingredients; defaults to 20, at most 100."`
}
type SuggestItemsOut struct {
	Items []training.NutritionItemSuggestion `json:"items"`
}
type DeleteNutritionInput struct {
	EntryID          int64 `json:"entry_id" jsonschema:"Id of the entry to delete."`
	ExpectedRevision *int  `json:"expected_revision,omitempty" jsonschema:"The entry's revision from your last read; rejected with a conflict if it changed since."`
}
type DeleteNutritionOut struct {
	EntryID int64 `json:"entry_id"`
	Deleted bool  `json:"deleted"`
}
type CopyNutritionInput struct {
	EntryID        int64  `json:"entry_id" jsonschema:"Id of the entry to copy."`
	Date           string `json:"date,omitempty" jsonschema:"Local date YYYY-MM-DD for the copy. Omit for today."`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Optional retry key. Same key + same payload replays; different payload is a conflict."`
}
type NutritionDayInput struct {
	Date string `json:"date,omitempty" jsonschema:"Local date YYYY-MM-DD. Omit for today. Do not combine with from/to."`
	From string `json:"from,omitempty" jsonschema:"First local date of a multi-day read (returns days[] instead of day). At most 366 days."`
	To   string `json:"to,omitempty" jsonschema:"Last local date of a multi-day read; defaults to from."`
}
type NutritionDayOut struct {
	Today    string                  `json:"today"`
	Timezone string                  `json:"timezone"`
	Day      *training.NutritionDay  `json:"day,omitempty"`
	Days     []training.NutritionDay `json:"days,omitempty"`
}
type TargetsInput struct {
	Date string `json:"date,omitempty" jsonschema:"Local date YYYY-MM-DD. Omit for today."`
}
type TargetsOut struct {
	Date    string                      `json:"date"`
	Targets training.NutritionTargets   `json:"targets"`
	History []training.NutritionTargets `json:"history"`
}
type SetTargetsInput struct {
	EffectiveFrom    string   `json:"effective_from,omitempty" jsonschema:"First local date YYYY-MM-DD the targets apply to; omit for today. They apply until the next change; earlier dates never change."`
	CaloriesKcal     *float64 `json:"calories_kcal,omitempty" jsonschema:"Daily energy target in kcal. Omit to keep the value in effect on effective_from."`
	ProteinG         *float64 `json:"protein_g,omitempty" jsonschema:"Daily protein target in grams. Omit to keep."`
	CarbsG           *float64 `json:"carbs_g,omitempty" jsonschema:"Daily carbohydrate target in grams. Omit to keep."`
	FatG             *float64 `json:"fat_g,omitempty" jsonschema:"Daily fat target in grams. Omit to keep."`
	Clear            []string `json:"clear,omitempty" jsonschema:"Targets to remove from effective_from on: any of calories_kcal, protein_g, carbs_g, fat_g."`
	ExpectedRevision *int     `json:"expected_revision,omitempty" jsonschema:"targets.revision from get_nutrition_targets or get_nutrition_day (0 when no targets were ever set); rejected with a conflict if targets changed since."`
}

func registerNutritionTools(r *registry, service *training.Service) {
	addSchema := inputSchema[AddNutritionInput]()
	setNonEmpty(addSchema, "name")
	setItemsSchema(prop(addSchema, "items"))
	setDate(addSchema, "date")
	addTool(r, &mcp.Tool{
		Name: "add_nutrition_entry",
		Description: "Log one food or meal eaten. Returns the persisted entry (id, date, name, values, items, incomplete_values, revision, created_at, updated_at) and replayed. " +
			nutritionUnits + " " + nutritionMissing + " " + nutritionItemsRule + " " + nutritionDates + " " + nutritionIdem,
		InputSchema: addSchema,
		Annotations: additive("Add nutrition entry", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in AddNutritionInput) (*mcp.CallToolResult, NutritionEntryOut, error) {
		e, replayed, err := service.AddNutritionEntry(ctx, training.NutritionInput{Date: in.Date, Name: in.Name, Nutrients: in.nutrients(), Items: nutritionItems(in.Items), IdempotencyKey: in.IdempotencyKey})
		return nil, NutritionEntryOut{Entry: e, Replayed: replayed}, toolError(err)
	})

	getSchema := inputSchema[EntryIDInput]()
	setPositiveID(getSchema, "entry_id")
	addTool(r, &mcp.Tool{
		Name:        "get_nutrition_entry",
		Description: "Read one nutrition entry by id, with its revision. Values are null when not recorded. " + nutritionUnits,
		InputSchema: getSchema,
		Annotations: readOnly("Get nutrition entry"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in EntryIDInput) (*mcp.CallToolResult, NutritionEntryOut, error) {
		e, err := service.GetNutritionEntry(ctx, in.EntryID)
		return nil, NutritionEntryOut{Entry: e}, toolError(err)
	})

	listSchema := inputSchema[ListNutritionInput]()
	setDate(listSchema, "date", "from", "to")
	addTool(r, &mcp.Tool{
		Name: "list_nutrition_entries",
		Description: "Find nutrition entries, newest first. With no filters returns the latest entries across all dates - use it (optionally with query) to find a previous meal before copy_nutrition_entry. " +
			"Filter by one date, or by an inclusive from/to range, and/or by query text in the name. " + nutritionUnits + " " + nutritionDates,
		InputSchema: listSchema,
		Annotations: readOnly("List nutrition entries"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListNutritionInput) (*mcp.CallToolResult, NutritionEntriesOut, error) {
		f := training.NutritionFilter{From: in.From, To: in.To, Query: in.Query, Limit: in.Limit}
		if in.Date != "" {
			if in.From != "" || in.To != "" {
				return nil, NutritionEntriesOut{}, toolError(invalidArg("date", "use either date or from/to, not both"))
			}
			f.From, f.To = in.Date, in.Date
		}
		v, err := service.ListNutritionEntries(ctx, f)
		return nil, NutritionEntriesOut{Entries: v}, toolError(err)
	})

	updSchema := inputSchema[UpdateNutritionInput]()
	setPositiveID(updSchema, "entry_id")
	setDate(updSchema, "date")
	prop(updSchema, "clear").Items.Enum = nutrientClearEnum
	updSchema.MinProperties = jsonschema.Ptr(2)
	setItemsSchema(prop(updSchema, "items"))
	addTool(r, &mcp.Tool{
		Name: "update_nutrition_entry",
		Description: "Correct one nutrition entry by id (read it first). Only the fields given change; omitted fields stay as stored. Use clear to turn a value back into 'not recorded'. Returns the persisted entry with its new revision. " +
			"For a meal made of items, change the ingredients by sending items with the WHOLE new list (values are recomputed from it); its values cannot be set directly unless items is replaced in the same call ([] removes the items and keeps the sums as directly editable values). " +
			nutritionUnits + " " + nutritionMissing + " " + revisionRule,
		InputSchema: updSchema,
		Annotations: destructive("Update nutrition entry", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in UpdateNutritionInput) (*mcp.CallToolResult, NutritionEntryOut, error) {
		p := training.NutritionPatch{Name: in.Name, Date: in.Date, Nutrients: in.nutrients(), ExpectedRevision: in.ExpectedRevision}
		if in.Items != nil {
			items := nutritionItems(*in.Items)
			if items == nil {
				items = []training.NutritionItem{}
			}
			p.Items = &items
		}
		if err := applyClear(in.Clear, &p.ClearCalories, &p.ClearProtein, &p.ClearCarbs, &p.ClearFat); err != nil {
			return nil, NutritionEntryOut{}, toolError(err)
		}
		e, err := service.UpdateNutritionEntry(ctx, in.EntryID, p)
		return nil, NutritionEntryOut{Entry: e}, toolError(err)
	})

	sugSchema := inputSchema[SuggestItemsInput]()
	addTool(r, &mcp.Tool{
		Name: "suggest_nutrition_items",
		Description: "Ingredients the user logged before (as items of earlier meals), with their latest values: name, amount_g and the values for that amount, last_used date and uses count, most recently used first. " +
			"Check it before logging a meal as items and reuse these values, scaling them by amount_g when the portion differs (e.g. 150 g when 200 g was logged: multiply by 0.75) - and tell the user you scaled. " +
			"A value that is null was not recorded; keep it omitted rather than guessing 0. " + nutritionUnits,
		InputSchema: sugSchema,
		Annotations: readOnly("Suggest nutrition items"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SuggestItemsInput) (*mcp.CallToolResult, SuggestItemsOut, error) {
		v, err := service.SuggestNutritionItems(ctx, in.Query, in.Limit)
		return nil, SuggestItemsOut{Items: v}, toolError(err)
	})

	delSchema := inputSchema[DeleteNutritionInput]()
	setPositiveID(delSchema, "entry_id")
	addTool(r, &mcp.Tool{
		Name:        "delete_nutrition_entry",
		Description: "Permanently delete one nutrition entry by id. Irreversible: confirm with the user first, naming the entry. " + revisionRule,
		InputSchema: delSchema,
		Annotations: destructive("Delete nutrition entry", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DeleteNutritionInput) (*mcp.CallToolResult, DeleteNutritionOut, error) {
		err := service.DeleteNutritionEntry(ctx, in.EntryID, in.ExpectedRevision)
		return nil, DeleteNutritionOut{EntryID: in.EntryID, Deleted: err == nil}, toolError(err)
	})

	copySchema := inputSchema[CopyNutritionInput]()
	setPositiveID(copySchema, "entry_id")
	setDate(copySchema, "date")
	addTool(r, &mcp.Tool{
		Name: "copy_nutrition_entry",
		Description: "Log an earlier meal again: creates a NEW, independent entry with the same name and values on date (today when omitted), with copied_from set. Editing or deleting the copy never changes the original, and vice versa. " +
			"Find the original first with list_nutrition_entries or get_nutrition_day. " + nutritionDates + " " + nutritionIdem,
		InputSchema: copySchema,
		Annotations: additive("Copy nutrition entry", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in CopyNutritionInput) (*mcp.CallToolResult, NutritionEntryOut, error) {
		e, replayed, err := service.CopyNutritionEntry(ctx, in.EntryID, in.Date, in.IdempotencyKey)
		return nil, NutritionEntryOut{Entry: e, Replayed: replayed}, toolError(err)
	})

	daySchema := inputSchema[NutritionDayInput]()
	setDate(daySchema, "date", "from", "to")
	addTool(r, &mcp.Tool{
		Name: "get_nutrition_day",
		Description: "Read one local day of nutrition (or several with from/to): the entries in logged order, consumed totals per nutrient, entries_missing_value per nutrient (entries with that value not recorded, so a total may be an undercount), " +
			"the targets in effect that day with their revision, and remaining/over per nutrient when a target exists. Always returns today and the timezone, so 'today'/'yesterday' resolve correctly. " +
			nutritionUnits + " " + nutritionDates,
		InputSchema: daySchema,
		Annotations: readOnly("Get nutrition day"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NutritionDayInput) (*mcp.CallToolResult, NutritionDayOut, error) {
		out := NutritionDayOut{Today: service.Today(), Timezone: service.Location().String()}
		if in.From != "" || in.To != "" {
			if in.Date != "" {
				return nil, out, toolError(invalidArg("date", "use either date or from/to, not both"))
			}
			if in.From == "" {
				return nil, out, toolError(invalidArg("from", "is required when to is given"))
			}
			days, err := service.NutritionDays(ctx, in.From, in.To)
			out.Days = days
			return nil, out, toolError(err)
		}
		d, err := service.NutritionDayOn(ctx, in.Date)
		if err != nil {
			return nil, out, toolError(err)
		}
		out.Day = &d
		return nil, out, nil
	})

	targetsSchema := inputSchema[TargetsInput]()
	setDate(targetsSchema, "date")
	addTool(r, &mcp.Tool{
		Name:        "get_nutrition_targets",
		Description: "Read the daily nutrition targets in effect on a local date (default today) with their revision (0 when none were ever set), plus the full history of target changes. A null target means none is set. " + nutritionUnits,
		InputSchema: targetsSchema,
		Annotations: readOnly("Get nutrition targets"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in TargetsInput) (*mcp.CallToolResult, TargetsOut, error) {
		date, err := service.ResolveDate(in.Date)
		if err != nil {
			return nil, TargetsOut{}, toolError(err)
		}
		t, err := service.NutritionTargetsOn(ctx, date)
		if err != nil {
			return nil, TargetsOut{Date: date}, toolError(err)
		}
		h, err := service.NutritionTargetHistory(ctx)
		return nil, TargetsOut{Date: date, Targets: t, History: h}, toolError(err)
	})

	setSchema := inputSchema[SetTargetsInput]()
	setDate(setSchema, "effective_from")
	prop(setSchema, "clear").Items.Enum = nutrientClearEnum
	addTool(r, &mcp.Tool{
		Name: "set_nutrition_targets",
		Description: "Set daily nutrition targets from effective_from (default today) until the next change. Earlier dates never change. Omitted values keep what is in effect on effective_from; clear removes a target. " +
			targetsRule + " Units: calories_kcal in kcal per day; protein_g, carbs_g, fat_g in grams per day. Dates are local YYYY-MM-DD in the configured timezone (default Asia/Jerusalem). Never creates a workout. " + revisionRule,
		InputSchema: setSchema,
		Annotations: destructive("Set nutrition targets", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SetTargetsInput) (*mcp.CallToolResult, TargetsOut, error) {
		p := training.TargetPatch{Nutrients: training.Nutrients{CaloriesKcal: in.CaloriesKcal, ProteinG: in.ProteinG, CarbsG: in.CarbsG, FatG: in.FatG}, ExpectedRevision: in.ExpectedRevision}
		if err := applyClear(in.Clear, &p.ClearCalories, &p.ClearProtein, &p.ClearCarbs, &p.ClearFat); err != nil {
			return nil, TargetsOut{}, toolError(err)
		}
		t, err := service.SetNutritionTargets(ctx, in.EffectiveFrom, p)
		if err != nil {
			return nil, TargetsOut{}, toolError(err)
		}
		h, err := service.NutritionTargetHistory(ctx)
		return nil, TargetsOut{Date: t.EffectiveFrom, Targets: t, History: h}, toolError(err)
	})
}

// setItemsSchema bounds an ingredient list and its members.
func setItemsSchema(s *jsonschema.Schema) {
	s.MaxItems = jsonschema.Ptr(training.MaxNutritionItems)
	setNonEmpty(s.Items, "name")
	prop(s.Items, "amount_g").ExclusiveMinimum = jsonschema.Ptr(0.0)
	prop(s.Items, "amount_g").Maximum = jsonschema.Ptr(float64(training.MaxItemAmountG))
}
