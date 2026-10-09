package constant

type TaskPlatform string

const (
	TaskPlatformSuno               TaskPlatform = "suno"
	TaskPlatformMidjourney                      = "mj"
	TaskPlatformLyria                           = "lyria"
	TaskPlatformVertexInteractions              = "vertex-interactions"
)

func IsInteractionsTaskPlatform(platform TaskPlatform) bool {
	return platform == TaskPlatformLyria || platform == TaskPlatformVertexInteractions
}

const (
	TaskActionImageToVideo     = "image_to_video"
	TaskActionTextToVideo      = "text_to_video"
	TaskActionFirstTailToVideo = "first_tail_to_video"
	TaskActionReferenceToVideo = "reference_to_video"
)

var legacyTaskActionAliases = map[string]string{"generate": TaskActionImageToVideo, "textGenerate": TaskActionTextToVideo, "firstTailGenerate": TaskActionFirstTailToVideo, "referenceGenerate": TaskActionReferenceToVideo, "remixGenerate": TaskActionRemix}
var TaskPluginEnabled = true
var TaskPluginOverrideEnabled = true

func NormalizeTaskAction(action string) string {
	if v, ok := legacyTaskActionAliases[action]; ok {
		return v
	}
	return action
}

const (
	SunoActionMusic  = "MUSIC"
	SunoActionLyrics = "LYRICS"

	TaskActionGenerate     = "generate"
	TaskActionTextGenerate = "textGenerate"
	// TaskActionGenerateContent is used by synchronous Gemini GenerateContent
	// requests. It must not be normalized to a legacy video action.
	TaskActionGenerateContent   = "generate_content"
	TaskActionFirstTailGenerate = "firstTailGenerate"
	TaskActionReferenceGenerate = "referenceGenerate"
	TaskActionRemix             = "remixGenerate"
	TaskActionOmniGenerate      = "omniGenerate" // kling omni-video
)

var SunoModel2Action = map[string]string{
	"suno_music":  SunoActionMusic,
	"suno_lyrics": SunoActionLyrics,
}
