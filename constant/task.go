package constant

type TaskPlatform string

const (
	TaskPlatformSuno       TaskPlatform = "suno"
	TaskPlatformMidjourney              = "mj"
	TaskPlatformLyria                   = "lyria"
)

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

	TaskActionGenerate          = "generate"
	TaskActionTextGenerate      = "textGenerate"
	TaskActionFirstTailGenerate = "firstTailGenerate"
	TaskActionReferenceGenerate = "referenceGenerate"
	TaskActionRemix             = "remixGenerate"
	TaskActionOmniGenerate      = "omniGenerate" // kling omni-video
)

var SunoModel2Action = map[string]string{
	"suno_music":  SunoActionMusic,
	"suno_lyrics": SunoActionLyrics,
}
