package transcribe

var whisperRuntime = &runtimeAsset{
	url:    "https://github.com/ggml-org/whisper.cpp/releases/download/" + whisperVersion + "/whisper-bin-x64.zip",
	size:   3_968_674,
	sha256: "d824b1e37599f882b396e73f1ee0bfd5d0529f700314c48311dcbd00b803321d",
	// whisper.dll loads the ggml libraries beside it; the release's executables are left out.
	files: []runtimeFile{
		{"Release/ggml-base.dll", "ggml-base.dll", 536_064, "61c9c57f49f380896ed54ed3f71735e10a2d986f482c2b0a55e72b47f731b193"},
		{"Release/ggml-cpu.dll", "ggml-cpu.dll", 683_008, "eb2f1d023e51c40b27463b5ab165600eb48cb60323740f400b1b62e1c8706c56"},
		{"Release/ggml.dll", "ggml.dll", 67_584, "bcfeaf6e5b59c315fc36e290715aa7a84b22461e968543c13dfcf57df7b1b81d"},
		{"Release/whisper.dll", "whisper.dll", 483_840, "4037a6567fbb08fc7efda18e4d128a95df9c31ba171af20439d1a93b785d007e"},
	},
}
