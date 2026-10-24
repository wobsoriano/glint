package transcribe

import (
	"testing"
	"unsafe"
)

// The expected numbers are bindgen's layout assertions for whisper.h 1.8.3 in
// whisper-rs-sys 0.15.0 (src/bindings.rs), the build the Rust app links.
func TestStructLayoutMatchesWhisperH(t *testing.T) {
	var f fullParams
	offsets := []struct {
		name      string
		got, want uintptr
	}{
		{"strategy", unsafe.Offsetof(f.strategy), 0},
		{"n_threads", unsafe.Offsetof(f.nThreads), 4},
		{"n_max_text_ctx", unsafe.Offsetof(f.nMaxTextCtx), 8},
		{"offset_ms", unsafe.Offsetof(f.offsetMs), 12},
		{"duration_ms", unsafe.Offsetof(f.durationMs), 16},
		{"translate", unsafe.Offsetof(f.translate), 20},
		{"no_context", unsafe.Offsetof(f.noContext), 21},
		{"no_timestamps", unsafe.Offsetof(f.noTimestamps), 22},
		{"single_segment", unsafe.Offsetof(f.singleSegment), 23},
		{"print_special", unsafe.Offsetof(f.printSpecial), 24},
		{"print_progress", unsafe.Offsetof(f.printProgress), 25},
		{"print_realtime", unsafe.Offsetof(f.printRealtime), 26},
		{"print_timestamps", unsafe.Offsetof(f.printTimestamps), 27},
		{"token_timestamps", unsafe.Offsetof(f.tokenTimestamps), 28},
		{"thold_pt", unsafe.Offsetof(f.tholdPt), 32},
		{"thold_ptsum", unsafe.Offsetof(f.tholdPtsum), 36},
		{"max_len", unsafe.Offsetof(f.maxLen), 40},
		{"split_on_word", unsafe.Offsetof(f.splitOnWord), 44},
		{"max_tokens", unsafe.Offsetof(f.maxTokens), 48},
		{"debug_mode", unsafe.Offsetof(f.debugMode), 52},
		{"audio_ctx", unsafe.Offsetof(f.audioCtx), 56},
		{"tdrz_enable", unsafe.Offsetof(f.tdrzEnable), 60},
		{"suppress_regex", unsafe.Offsetof(f.suppressRegex), 64},
		{"initial_prompt", unsafe.Offsetof(f.initialPrompt), 72},
		{"carry_initial_prompt", unsafe.Offsetof(f.carryInitial), 80},
		{"prompt_tokens", unsafe.Offsetof(f.promptTokens), 88},
		{"prompt_n_tokens", unsafe.Offsetof(f.promptNTokens), 96},
		{"language", unsafe.Offsetof(f.language), 104},
		{"detect_language", unsafe.Offsetof(f.detectLanguage), 112},
		{"suppress_blank", unsafe.Offsetof(f.suppressBlank), 113},
		{"suppress_nst", unsafe.Offsetof(f.suppressNst), 114},
		{"temperature", unsafe.Offsetof(f.temperature), 116},
		{"max_initial_ts", unsafe.Offsetof(f.maxInitialTs), 120},
		{"length_penalty", unsafe.Offsetof(f.lengthPenalty), 124},
		{"temperature_inc", unsafe.Offsetof(f.temperatureInc), 128},
		{"entropy_thold", unsafe.Offsetof(f.entropyThold), 132},
		{"logprob_thold", unsafe.Offsetof(f.logprobThold), 136},
		{"no_speech_thold", unsafe.Offsetof(f.noSpeechThold), 140},
		{"greedy", unsafe.Offsetof(f.greedyBestOf), 144},
		{"beam_search", unsafe.Offsetof(f.beamSize), 148},
		{"new_segment_callback", unsafe.Offsetof(f.newSegmentCallback), 160},
		{"new_segment_callback_user_data", unsafe.Offsetof(f.newSegmentUserData), 168},
		{"progress_callback", unsafe.Offsetof(f.progressCallback), 176},
		{"progress_callback_user_data", unsafe.Offsetof(f.progressUserData), 184},
		{"encoder_begin_callback", unsafe.Offsetof(f.encoderBeginCallback), 192},
		{"encoder_begin_callback_user_data", unsafe.Offsetof(f.encoderBeginUserData), 200},
		{"abort_callback", unsafe.Offsetof(f.abortCallback), 208},
		{"abort_callback_user_data", unsafe.Offsetof(f.abortUserData), 216},
		{"logits_filter_callback", unsafe.Offsetof(f.logitsFilterCallback), 224},
		{"logits_filter_callback_user_data", unsafe.Offsetof(f.logitsFilterUserData), 232},
		{"grammar_rules", unsafe.Offsetof(f.grammarRules), 240},
		{"n_grammar_rules", unsafe.Offsetof(f.nGrammarRules), 248},
		{"i_start_rule", unsafe.Offsetof(f.iStartRule), 256},
		{"grammar_penalty", unsafe.Offsetof(f.grammarPenalty), 264},
		{"vad", unsafe.Offsetof(f.vad), 268},
		{"vad_model_path", unsafe.Offsetof(f.vadModelPath), 272},
		{"vad_params", unsafe.Offsetof(f.vadParams), 280},
	}
	for _, o := range offsets {
		if o.got != o.want {
			t.Errorf("whisper_full_params.%s at %d, want %d", o.name, o.got, o.want)
		}
	}
	var c contextParams
	var d tokenData
	sizes := []struct {
		name      string
		got, want uintptr
	}{
		{"whisper_full_params", unsafe.Sizeof(f), 304},
		{"whisper_vad_params", unsafe.Sizeof(f.vadParams), 24},
		{"whisper_context_params", unsafe.Sizeof(c), 48},
		{"whisper_context_params.dtw_aheads", unsafe.Offsetof(c.dtwAheadsNHeads), 24},
		{"whisper_context_params.dtw_mem_size", unsafe.Offsetof(c.dtwMemSize), 40},
		{"whisper_token_data", unsafe.Sizeof(d), 56},
		{"whisper_token_data.t0", unsafe.Offsetof(d.t0), 24},
		{"whisper_token_data.vlen", unsafe.Offsetof(d.vlen), 48},
	}
	for _, s := range sizes {
		if s.got != s.want {
			t.Errorf("%s is %d, want %d", s.name, s.got, s.want)
		}
	}
}
