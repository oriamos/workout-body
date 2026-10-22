package mcpsrv

import (
	"context"
	"errors"

	"github.com/fullfran/training-mcp/internal/training"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	weightUnits = "Body weight is in kilograms (20-400 kg). If the user gives pounds, convert to kg yourself (1 lb = 0.45359237 kg) and tell them you converted."
	weightDates = "Dates are local calendar dates YYYY-MM-DD in the configured timezone (default Asia/Jerusalem); omit date for today there. A weigh-in cannot be logged for a future date. Body weight never creates or changes a workout."
	weightTrend = "Weeks follow the configured week (Sunday to Saturday by default; WORKOUT_WEEK_START); week_start/week_end give each week's dates. A week's average_kg (and min_kg/max_kg) uses only the days that have a weigh-in (days_logged says how many); change_kg/change_pct compare it with the previous week's average. " +
		"A null average or change means there is not enough data (no weigh-in that week or the week before) - report it as missing and never invent, estimate or interpolate a number for it. trend is up, down or flat (|change_kg| < 0.1 kg)."
)

type LogWeighInInput struct {
	WeightKG         float64 `json:"weight_kg" jsonschema:"Body weight in kilograms, 20-400. Convert pounds to kg first."`
	Date             string  `json:"date,omitempty" jsonschema:"Local date YYYY-MM-DD of the weigh-in. Omit for today."`
	Note             string  `json:"note,omitempty" jsonschema:"Optional short note the user gave (at most 200 characters), e.g. 'after travel'."`
	ExpectedRevision *int    `json:"expected_revision,omitempty" jsonschema:"Optional. The weigh-in's revision from your last read; 0 means the date must not have a weigh-in yet. Rejected with a conflict if it does not match."`
}
type WeighInOut struct {
	WeighIn  *training.WeighIn `json:"weigh_in,omitempty"`
	Created  bool              `json:"created"`
	Today    string            `json:"today"`
	Timezone string            `json:"timezone"`
}
type GetWeighInInput struct {
	Date string `json:"date,omitempty" jsonschema:"Local date YYYY-MM-DD. Omit for today."`
}
type GetWeighInOut struct {
	Date     string            `json:"date"`
	Found    bool              `json:"found"`
	WeighIn  *training.WeighIn `json:"weigh_in,omitempty"`
	Today    string            `json:"today"`
	Timezone string            `json:"timezone"`
}
type DeleteWeighInInput struct {
	Date             string `json:"date" jsonschema:"Local date YYYY-MM-DD of the weigh-in to delete."`
	ExpectedRevision *int   `json:"expected_revision,omitempty" jsonschema:"The weigh-in's revision from your last read; rejected with a conflict if it changed since."`
}
type DeleteWeighInOut struct {
	Date    string `json:"date"`
	Deleted bool   `json:"deleted"`
}
type WeightTrendInput struct {
	To    string `json:"to,omitempty" jsonschema:"Local date YYYY-MM-DD the view ends on (its week is the last one). Omit for today."`
	Weeks int    `json:"weeks,omitempty" jsonschema:"How many weeks to return, 1-104. Defaults to 8."`
}
type WeightTrendOut struct {
	training.WeightTrend
	Timezone string `json:"timezone"`
}

func registerWeightTools(r *registry, service *training.Service) {
	logSchema := inputSchema[LogWeighInInput]()
	setDate(logSchema, "date")
	prop(logSchema, "expected_revision").Minimum = jsonschema.Ptr(0.0)
	addTool(r, &mcp.Tool{
		Name: "log_weigh_in",
		Description: "Record the user's body weight for a date (today when omitted). There is at most one weigh-in per local date: logging a date that already has one REPLACES it, and sending the same values again changes nothing, so a retry after a timeout is harmless and needs no key. " +
			"Returns the stored weigh_in (date, weight_kg, note, revision), created (false when it replaced or matched an existing weigh-in), today and timezone. " + weightUnits + " " + weightDates,
		InputSchema: logSchema,
		Annotations: destructive("Log weigh-in", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in LogWeighInInput) (*mcp.CallToolResult, WeighInOut, error) {
		out := WeighInOut{Today: service.Today(), Timezone: service.Location().String()}
		w, created, err := service.LogWeighIn(ctx, in.Date, in.WeightKG, in.Note, in.ExpectedRevision)
		if err != nil {
			return nil, out, toolError(err)
		}
		out.WeighIn, out.Created = &w, created
		return nil, out, nil
	})

	getSchema := inputSchema[GetWeighInInput]()
	setDate(getSchema, "date")
	addTool(r, &mcp.Tool{
		Name: "get_weigh_in",
		Description: "Read the weigh-in of one local date (today when omitted). found=false (and no weigh_in) when nothing was logged that day - do not make a value up. Always returns today and the timezone. " +
			"For several days or how the weight is going, use get_weight_trend. " + weightUnits,
		InputSchema: getSchema,
		Annotations: readOnly("Get weigh-in"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetWeighInInput) (*mcp.CallToolResult, GetWeighInOut, error) {
		out := GetWeighInOut{Today: service.Today(), Timezone: service.Location().String()}
		date, err := service.ResolveDate(in.Date)
		if err != nil {
			return nil, out, toolError(err)
		}
		out.Date = date
		w, err := service.GetWeighIn(ctx, date)
		if errors.Is(err, training.ErrNotFound) {
			return nil, out, nil
		}
		if err != nil {
			return nil, out, toolError(err)
		}
		out.Found, out.WeighIn = true, &w
		return nil, out, nil
	})

	delSchema := inputSchema[DeleteWeighInInput]()
	setDate(delSchema, "date")
	addTool(r, &mcp.Tool{
		Name:        "delete_weigh_in",
		Description: "Permanently delete the weigh-in of one local date. Irreversible: confirm with the user first, naming the date and the weight. To correct a value, call log_weigh_in for that date instead (it replaces). " + revisionRule,
		InputSchema: delSchema,
		Annotations: destructive("Delete weigh-in", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DeleteWeighInInput) (*mcp.CallToolResult, DeleteWeighInOut, error) {
		err := service.DeleteWeighIn(ctx, in.Date, in.ExpectedRevision)
		return nil, DeleteWeighInOut{Date: in.Date, Deleted: err == nil}, toolError(err)
	})

	trendSchema := inputSchema[WeightTrendInput]()
	setDate(trendSchema, "to")
	prop(trendSchema, "weeks").Minimum = jsonschema.Ptr(1.0)
	prop(trendSchema, "weeks").Maximum = jsonschema.Ptr(104.0)
	addTool(r, &mcp.Tool{
		Name: "get_weight_trend",
		Description: "How the user's body weight is going: the last `weeks` weeks (default 8) ending with the week that contains `to` (default today), oldest first, each with days_logged, average_kg, min_kg, max_kg, change_kg, change_pct and trend; plus the weigh-ins in the range, latest (the most recent weigh-in on or before `to`) and overall_change_kg (last week with data minus the first one). " +
			weightTrend + " Daily weight fluctuates; the weekly average is the signal. Read-only. " + weightUnits,
		InputSchema: trendSchema,
		Annotations: readOnly("Get weight trend"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in WeightTrendInput) (*mcp.CallToolResult, WeightTrendOut, error) {
		t, err := service.WeightTrendFor(ctx, in.To, in.Weeks)
		return nil, WeightTrendOut{WeightTrend: t, Timezone: service.Location().String()}, toolError(err)
	})
}
