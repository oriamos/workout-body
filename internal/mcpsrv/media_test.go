package mcpsrv

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/training-mcp/internal/mediastore"
	"github.com/fullfran/training-mcp/internal/sqlitestore"
	"github.com/fullfran/training-mcp/internal/training"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newMediaService is the real service with video storage in a temp dir.
func newMediaService(t *testing.T) *training.Service {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlitestore.Open(filepath.Join(dir, "training.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := mediastore.New(filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	return training.NewService(st, func() time.Time { return testNow }).
		WithLocation(mustLoc(t, "Asia/Jerusalem")).
		WithMedia(st, blobs, 1<<20)
}

// fakeMP4 is a synthetic byte stream with an MP4 container signature.
func fakeMP4() *strings.Reader {
	return strings.NewReader("\x00\x00\x00\x18ftypisom" + strings.Repeat("\x00", 64))
}

func mediaTransports(t *testing.T, fn func(t *testing.T, svc *training.Service, cs *mcp.ClientSession)) {
	t.Run("in-memory", func(t *testing.T) {
		svc := newMediaService(t)
		fn(t, svc, connectInMemory(t, New(svc)))
	})
	t.Run("streamable-http", func(t *testing.T) {
		svc := newMediaService(t)
		fn(t, svc, connectHTTP(t, New(svc)))
	})
}

func TestExerciseGuideAndVideos(t *testing.T) {
	mediaTransports(t, func(t *testing.T, svc *training.Service, cs *mcp.ClientSession) {
		ctx := context.Background()
		var r training.BatchResult
		callOK(t, cs, "log_sets", map[string]any{"sets": []any{
			map[string]any{"exercise": "romanian deadlift", "weight_kg": 100, "reps": 8},
			map[string]any{"exercise": "romanian deadlift", "weight_kg": 100, "reps": 8},
		}}, &r)
		var ex ExerciseOut
		callOK(t, cs, "add_exercise_alias", map[string]any{"exercise": "romanian deadlift", "alias": "RDL"}, &ex)

		var g GuideOut
		callOK(t, cs, "get_exercise_guide", map[string]any{"exercise": "rdl"}, &g)
		if g.Guide.Exercise != "romanian deadlift" || g.Guide.Instructions != "" || len(g.Guide.Videos) != 0 {
			t.Fatalf("empty guide = %+v", g.Guide)
		}
		callOK(t, cs, "set_exercise_instructions", map[string]any{"exercise": "RDL", "instructions": "  Soft knees, hinge at the hips, bar close to the legs.  "}, &g)
		if g.Guide.ExerciseID != ex.Exercise.ID || g.Guide.Instructions != "Soft knees, hinge at the hips, bar close to the legs." {
			t.Fatalf("set instructions = %+v", g.Guide)
		}
		exVideo, err := svc.AttachVideo(ctx, training.OwnerExercise, "rdl", "demo.mp4", "side view", fakeMP4())
		if err != nil {
			t.Fatal(err)
		}
		callOK(t, cs, "get_exercise_guide", map[string]any{"exercise": strconv.FormatInt(ex.Exercise.ID, 10)}, &g)
		if g.Guide.Instructions == "" || len(g.Guide.Videos) != 1 || g.Guide.Videos[0].ID != exVideo.ID || g.Guide.Videos[0].ContentType != "video/mp4" {
			t.Fatalf("guide round trip = %+v", g.Guide)
		}
		res := call(t, cs, "get_exercise_guide", map[string]any{"exercise": "rdl"})
		if strings.Contains(structured(t, res), "storage_key") || strings.Contains(structured(t, res), exVideo.StorageKey) {
			t.Fatalf("guide leaks the storage key: %s", structured(t, res))
		}

		msg := callErr(t, cs, "set_exercise_instructions", map[string]any{"exercise": "rdl", "instructions": strings.Repeat("x", training.MaxInstructionsLen+1)})
		mustContain(t, msg, "instructions", "5000")
		msg = callErr(t, cs, "get_exercise_guide", map[string]any{"exercise": "no such lift"})
		mustContain(t, msg, "does not exist")

		set1, set2 := r.Sets[0].ID, r.Sets[1].ID
		v1, err := svc.AttachVideo(ctx, training.OwnerSet, strconv.FormatInt(set1, 10), "set1.mp4", "", fakeMP4())
		if err != nil {
			t.Fatal(err)
		}
		v2, err := svc.AttachVideo(ctx, training.OwnerSet, strconv.FormatInt(set2, 10), "set2.mp4", "", fakeMP4())
		if err != nil {
			t.Fatal(err)
		}
		var vids VideosOut
		callOK(t, cs, "list_set_videos", map[string]any{"set_id": set1}, &vids)
		if len(vids.Videos) != 1 || vids.Videos[0].ID != v1.ID || vids.Videos[0].Filename != "set1.mp4" {
			t.Fatalf("list_set_videos = %+v", vids)
		}

		var del DeleteVideoOut
		callOK(t, cs, "delete_video", map[string]any{"media_id": v1.ID}, &del)
		if !del.Deleted || del.Video.OwnerType != "set" || del.Video.OwnerID != set1 {
			t.Fatalf("delete_video = %+v", del)
		}
		callOK(t, cs, "list_set_videos", map[string]any{"set_id": set1}, &vids)
		if len(vids.Videos) != 0 {
			t.Fatalf("video survived delete: %+v", vids)
		}
		msg = callErr(t, cs, "delete_video", map[string]any{"media_id": v1.ID})
		mustContain(t, msg, "not found")

		// Exercise videos are deleted by the same tool.
		callOK(t, cs, "delete_video", map[string]any{"media_id": exVideo.ID}, &del)
		if del.Video.OwnerType != "exercise" {
			t.Fatalf("delete exercise video = %+v", del)
		}

		// Deleting a set removes its videos.
		callOK(t, cs, "delete_set", map[string]any{"set_id": set2}, nil)
		if _, err := svc.GetMedia(ctx, v2.ID); err == nil {
			t.Fatal("video of a deleted set still exists")
		}
		msg = callErr(t, cs, "list_set_videos", map[string]any{"set_id": set2})
		mustContain(t, msg, "does not exist")
	})
}

func TestMediaDisabledErrors(t *testing.T) {
	cs := connectInMemory(t, New(newService(t, "")))
	var r training.BatchResult
	callOK(t, cs, "log_sets", map[string]any{"sets": []any{map[string]any{"exercise": "squat", "weight_kg": 100, "reps": 5}}}, &r)

	// The guide still works without media storage; it is simply empty.
	var g GuideOut
	callOK(t, cs, "get_exercise_guide", map[string]any{"exercise": "squat"}, &g)
	if g.Guide.Instructions != "" || len(g.Guide.Videos) != 0 {
		t.Fatalf("guide = %+v", g.Guide)
	}
	for name, args := range map[string]map[string]any{
		"list_set_videos":           {"set_id": r.Sets[0].ID},
		"delete_video":              {"media_id": 1},
		"set_exercise_instructions": {"exercise": "squat", "instructions": "brace"},
	} {
		msg := callErr(t, cs, name, args)
		mustContain(t, msg, "media storage is not configured", "Tell the user")
		if strings.Contains(msg, "internal tool error") {
			t.Fatalf("%s masked: %q", name, msg)
		}
	}
}

func TestSessionPrescriptionRevisionAndClear(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		var r training.BatchResult
		callOK(t, cs, "log_sets", map[string]any{"prescriptions": []any{
			map[string]any{"exercise": "squat", "target_sets": 3, "target_weight_kg": 120, "target_rir": 2},
			map[string]any{"exercise": "lunge", "target_sets": 2},
		}}, &r)
		sid, rev := r.Workout.ID, r.Workout.Revision
		if len(r.Sets) != 0 || len(r.Workout.Sets) != 0 {
			t.Fatalf("prescriptions created completed sets: %+v", r)
		}

		msg := callErr(t, cs, "adjust_session_exercise", map[string]any{"session_id": sid, "exercise": "squat", "target_sets": 4, "expected_revision": rev - 1})
		mustContain(t, msg, "conflict", "re-read")

		var item ItemOut
		callOK(t, cs, "adjust_session_exercise", map[string]any{"session_id": sid, "exercise": "squat", "clear": []any{"target_weight_kg", "target_rir"}, "expected_revision": rev}, &item)
		for _, p := range item.Progress {
			if p.Exercise == "squat" && (p.TargetWeightKG != nil || p.TargetRIR != nil || p.TargetSets != 3) {
				t.Fatalf("cleared squat = %+v", p)
			}
		}
		msg = callErr(t, cs, "adjust_session_exercise", map[string]any{"session_id": sid, "exercise": "squat", "target_rir": 1, "clear": []any{"target_rir"}})
		mustContain(t, msg, "target_rir", "cannot both set a value and clear it")

		w := getWorkout(t, cs, "")
		msg = callErr(t, cs, "remove_session_exercise", map[string]any{"session_id": sid, "exercise": "lunge", "expected_revision": rev})
		mustContain(t, msg, "conflict")
		callOK(t, cs, "remove_session_exercise", map[string]any{"session_id": sid, "exercise": "lunge", "expected_revision": w.Workout.Revision}, &item)
		if len(item.Progress) != 1 {
			t.Fatalf("progress after remove = %+v", item.Progress)
		}
		if after := getWorkout(t, cs, ""); after.Workout.Revision <= w.Workout.Revision {
			t.Fatalf("remove did not bump revision: %d -> %d", w.Workout.Revision, after.Workout.Revision)
		}
	})
}
