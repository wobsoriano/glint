package transcribe

// Mirrors of whisper.h at whisperVersion. Field order and types follow the C structs so Go's
// layout matches the C ABI on 64-bit Windows and macOS; layout_test.go pins every offset to
// the ones bindgen computed for whisper-rs-sys.

const samplingGreedy = 0

type contextParams struct {
	useGPU             bool
	flashAttn          bool
	gpuDevice          int32
	dtwTokenTimestamps bool
	dtwAheadsPreset    int32
	dtwNTop            int32
	dtwAheadsNHeads    uintptr
	dtwAheadsHeads     uintptr
	dtwMemSize         uintptr
}

type vadParams struct {
	threshold            float32
	minSpeechDurationMs  int32
	minSilenceDurationMs int32
	maxSpeechDurationS   float32
	speechPadMs          int32
	samplesOverlap       float32
}

type fullParams struct {
	strategy        int32
	nThreads        int32
	nMaxTextCtx     int32
	offsetMs        int32
	durationMs      int32
	translate       bool
	noContext       bool
	noTimestamps    bool
	singleSegment   bool
	printSpecial    bool
	printProgress   bool
	printRealtime   bool
	printTimestamps bool
	tokenTimestamps bool
	tholdPt         float32
	tholdPtsum      float32
	maxLen          int32
	splitOnWord     bool
	maxTokens       int32
	debugMode       bool
	audioCtx        int32
	tdrzEnable      bool
	suppressRegex   *byte
	initialPrompt   *byte
	carryInitial    bool
	promptTokens    uintptr
	promptNTokens   int32
	language        *byte
	detectLanguage  bool
	suppressBlank   bool
	suppressNst     bool
	temperature     float32
	maxInitialTs    float32
	lengthPenalty   float32
	temperatureInc  float32
	entropyThold    float32
	logprobThold    float32
	noSpeechThold   float32
	greedyBestOf    int32
	beamSize        int32
	beamPatience    float32

	newSegmentCallback   uintptr
	newSegmentUserData   uintptr
	progressCallback     uintptr
	progressUserData     uintptr
	encoderBeginCallback uintptr
	encoderBeginUserData uintptr
	abortCallback        uintptr
	abortUserData        uintptr
	logitsFilterCallback uintptr
	logitsFilterUserData uintptr

	grammarRules   uintptr
	nGrammarRules  uintptr
	iStartRule     uintptr
	grammarPenalty float32

	vad          bool
	vadModelPath *byte
	vadParams    vadParams
}

type tokenData struct {
	id    int32
	tid   int32
	p     float32
	plog  float32
	pt    float32
	ptsum float32
	t0    int64
	t1    int64
	tDtw  int64
	vlen  float32
}

// whisperLib is the slice of the whisper.cpp C API the engine uses, bound by the platform
// loader.
type whisperLib struct {
	initFromFile   func(path string, p *contextParams) uintptr
	free           func(ctx uintptr)
	isMultilingual func(ctx uintptr) bool
	tokenEOT       func(ctx uintptr) int32
	initState      func(ctx uintptr) uintptr
	freeState      func(st uintptr)
	defaultParams  func(strategy int32) *fullParams
	full           func(ctx, st uintptr, p *fullParams, samples []float32) int32
	nSegments      func(st uintptr) int
	nTokens        func(st uintptr, seg int) int
	tokenText      func(ctx, st uintptr, seg, tok int) []byte
	tokenData      func(st uintptr, seg, tok int) tokenData
	langID         func(st uintptr) int32
	langStr        func(id int32) string

	// C function pointers that forward to onProgress and onAbort.
	progressCallback uintptr
	abortCallback    uintptr
}
