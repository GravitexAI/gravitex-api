package ratio_setting

import "testing"

func TestResolveVideoCompletionRatioForBillingUsesAudioDimensionForSeedance15(t *testing.T) {
	if err := UpdateVideoRatioByJSONString(`{"seedance-1-5-pro-251215":0}`); err != nil {
		t.Fatalf("UpdateVideoRatioByJSONString failed: %v", err)
	}
	if err := UpdateVideoCompletionRatioByJSONString(`{"seedance-1-5-pro-251215":{"noAudio":1.2,"audio":2.4}}`); err != nil {
		t.Fatalf("UpdateVideoCompletionRatioByJSONString failed: %v", err)
	}

	value, ok, dimension := ResolveVideoCompletionRatioForBilling("seedance-1-5-pro-251215", true, false, "720p", false)
	if !ok {
		t.Fatalf("ResolveVideoCompletionRatioForBilling returned ok=false")
	}
	if dimension != "audio" {
		t.Fatalf("dimension = %q, want %q", dimension, "audio")
	}
	if value != 1.2 {
		t.Fatalf("value = %v, want %v", value, 1.2)
	}
}

func TestResolveVideoCompletionRatioForBillingUsesResolutionDimensionForSeedance20(t *testing.T) {
	if err := UpdateVideoRatioByJSONString(`{}`); err != nil {
		t.Fatalf("UpdateVideoRatioByJSONString failed: %v", err)
	}
	if err := UpdateVideoCompletionRatioByJSONString(`{"seedance-2-0":{"480p":{"noVideo":7,"video":4.3},"720p":{"noVideo":7,"video":4.3},"1080p":{"noVideo":7.7,"video":4.7}}}`); err != nil {
		t.Fatalf("UpdateVideoCompletionRatioByJSONString failed: %v", err)
	}

	value, ok, dimension := ResolveVideoCompletionRatioForBilling("seedance-2-0", true, false, "720p", false)
	if !ok {
		t.Fatalf("ResolveVideoCompletionRatioForBilling returned ok=false")
	}
	if dimension != "video_input+resolution" {
		t.Fatalf("dimension = %q, want %q", dimension, "video_input+resolution")
	}
	if value != 7 {
		t.Fatalf("value = %v, want %v", value, 7.0)
	}
}
