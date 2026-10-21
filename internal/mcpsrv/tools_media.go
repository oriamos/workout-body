package mcpsrv

import (
	"context"

	"github.com/fullfran/training-mcp/internal/training"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const videoRule = "Videos are uploaded only through the HTTP API (POST /api/v1/sets/{id}/videos for a performed set, POST /api/v1/exercises/{ref}/videos for an exercise how-to), typically from the app; there is no MCP upload tool. " +
	"Tools see video metadata only (id, filename, content type, size, caption, date), never the video content: do not describe or judge what a video shows. " +
	"Video storage can be disabled on the server; then these tools report that media storage is not configured."

type GuideInput struct {
	Exercise string `json:"exercise" jsonschema:"Exercise name, alias or numeric id."`
}
type GuideOut struct {
	Guide training.ExerciseGuide `json:"guide"`
}
type InstructionsInput struct {
	Exercise     string `json:"exercise" jsonschema:"Existing exercise name, alias or numeric id (never created here)."`
	Instructions string `json:"instructions" jsonschema:"Written how-to text exactly as the user provided or approved it, at most 5000 characters. Empty string clears it."`
}
type VideosOut struct {
	SetID  int64            `json:"set_id"`
	Videos []training.Media `json:"videos"`
}
type MediaInput struct {
	MediaID int64 `json:"media_id" jsonschema:"Id of the video (from list_set_videos or get_exercise_guide)."`
}
type DeleteVideoOut struct {
	MediaID int64          `json:"media_id"`
	Deleted bool           `json:"deleted"`
	Video   training.Media `json:"video"`
}

func registerMediaTools(r *registry, service *training.Service) {
	guideSchema := inputSchema[GuideInput]()
	setNonEmpty(guideSchema, "exercise")
	addTool(r, &mcp.Tool{
		Name: "get_exercise_guide",
		Description: "Read how to perform an exercise: the user's stored written instructions (empty string when none exist) and metadata of attached how-to videos. " +
			"Answer 'how do I do X' from this; when instructions are empty, say no guide is stored rather than presenting invented technique advice as the user's guide. " + videoRule,
		InputSchema: guideSchema,
		Annotations: readOnly("Get exercise guide"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GuideInput) (*mcp.CallToolResult, GuideOut, error) {
		g, err := service.ExerciseGuide(ctx, in.Exercise)
		return nil, GuideOut{Guide: g}, toolError(err)
	})

	instrSchema := inputSchema[InstructionsInput]()
	setNonEmpty(instrSchema, "exercise")
	addTool(r, &mcp.Tool{
		Name: "set_exercise_instructions",
		Description: "Store (replace) the written how-to for an existing exercise; an empty string clears it. Write only text the user dictated or explicitly approved - never invent coaching content the user did not provide. " +
			"Returns the updated guide. Requires media storage to be configured.",
		InputSchema: instrSchema,
		Annotations: destructive("Set exercise instructions", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in InstructionsInput) (*mcp.CallToolResult, GuideOut, error) {
		g, err := service.SetExerciseInstructions(ctx, in.Exercise, in.Instructions)
		return nil, GuideOut{Guide: g}, toolError(err)
	})

	listSchema := inputSchema[SetIDInput]()
	setPositiveID(listSchema, "set_id")
	addTool(r, &mcp.Tool{
		Name:        "list_set_videos",
		Description: "List metadata of the videos attached to one performed set (by its stable set id). " + videoRule,
		InputSchema: listSchema,
		Annotations: readOnly("List set videos"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SetIDInput) (*mcp.CallToolResult, VideosOut, error) {
		v, err := service.SetMedia(ctx, in.SetID)
		return nil, VideosOut{SetID: in.SetID, Videos: v}, toolError(err)
	})

	delSchema := inputSchema[MediaInput]()
	setPositiveID(delSchema, "media_id")
	addTool(r, &mcp.Tool{
		Name: "delete_video",
		Description: "Permanently delete one video (of a set or of an exercise) and its stored file. Irreversible: confirm with the user first, naming the video (filename/caption and what it is attached to). " +
			"Deleting a set or workout already removes its videos. " + videoRule,
		InputSchema: delSchema,
		Annotations: destructive("Delete video", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in MediaInput) (*mcp.CallToolResult, DeleteVideoOut, error) {
		m, err := service.GetMedia(ctx, in.MediaID)
		if err != nil {
			return nil, DeleteVideoOut{MediaID: in.MediaID}, toolError(err)
		}
		err = service.DeleteMedia(ctx, in.MediaID)
		return nil, DeleteVideoOut{MediaID: in.MediaID, Deleted: err == nil, Video: m}, toolError(err)
	})
}
