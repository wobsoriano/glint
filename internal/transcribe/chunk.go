package transcribe

import "slices"

// soundRun is a stretch of 10 ms frames, [start, end), that holds sound rather than a pause.
type soundRun struct{ start, end int }

func (r soundRun) startMs() int64 { return int64(r.start) * frameMs }
func (r soundRun) endMs() int64   { return int64(r.end) * frameMs }

const (
	// A frame is sound when it is quietAboveFloorDb above the noise floor and within
	// soundBelowPeakDb of the loudest frame, which keeps the faintest hiss of a digitally silent
	// recording a pause.
	soundBelowPeakDb = 60
	// Quiet shorter than this is part of a run, such as the gaps between words.
	minPauseFrames = 20
	// whisper hears 30 s at a time.
	windowFrames = 3000
	// A window takes in this much of the pauses around its sound. With 450 ms of lead-in,
	// base.en skipped the start of wordtimecheck's fillers clip as if the disfluent prompt had
	// already said it, and with 300 ms tiny.en's words on the silence clip came out 175 ms off.
	padFrames = 15
	// A pause this long ends a window: tiny.en's DTW drifts words seconds into a long pause.
	// Shorter pauses stay inside a window, since a window per sentence loses context and costs
	// an encoder pass each.
	splitPauseFrames = 300
	// A word's DTW time may land this far outside its run and still count as heard there.
	coverSlackMs = 200
	// Unheard sound shorter than this between heard words is left alone, so a short run whose
	// one word DTW placed in the next pause does not cost its window a second decode.
	minHoleMs = 1000
)

// soundRuns finds the sound in levels, joined across pauses shorter than minPauseFrames. A run
// too long for one whisper window is cut at its quietest frame.
func soundRuns(levels []float64, floor float64) []soundRun {
	if len(levels) == 0 {
		return nil
	}
	threshold := max(floor+quietAboveFloorDb, slices.Max(levels)-soundBelowPeakDb)
	var runs []soundRun
	for f, l := range levels {
		if l < threshold {
			continue
		}
		if n := len(runs); n > 0 && f-runs[n-1].end < minPauseFrames {
			runs[n-1].end = f + 1
		} else {
			runs = append(runs, soundRun{f, f + 1})
		}
	}
	maxLen := windowFrames - 2*padFrames
	var out []soundRun
	for _, r := range runs {
		for r.end-r.start > maxLen {
			cut := r.start + maxLen/2
			for f := cut; f < r.start+maxLen; f++ {
				if levels[f] < levels[cut] {
					cut = f
				}
			}
			out = append(out, soundRun{r.start, cut})
			r.start = cut
		}
		out = append(out, r)
	}
	return out
}

// nextChunk groups runs[lo:hi], from lo, into what whisper should hear in one window: as many
// runs as fit, up to a long pause. It returns how many runs it took and the frames to decode,
// which reach into the pauses on either side but never past their middle, so no other run's
// sound is in the window. total is the number of frames.
func nextChunk(runs []soundRun, lo, hi, total int) (n, from, to int) {
	from = max(runs[lo].start-padFrames, 0)
	if lo > 0 {
		from = max(from, (runs[lo-1].end+runs[lo].start+1)/2)
	}
	n = 1
	for lo+n < hi && runs[lo+n].end+padFrames-from <= windowFrames && runs[lo+n].start-runs[lo+n-1].end < splitPauseFrames {
		n++
	}
	last := runs[lo+n-1]
	to = min(last.end+padFrames, total)
	if lo+n < len(runs) {
		to = min(to, (last.end+runs[lo+n].start)/2)
	}
	return n, from, to
}

// unheard compares runs, decoded in one window, with the sorted times of the words heard in
// them. Whisper sometimes stops a window early, or skips ahead past speech that resembles its
// prompt. done is how many leading runs the decode covered, through the last run with a word,
// and holes are the stretches before that run with minHoleMs of sound and no word, as index
// ranges into runs. A decode with no word on any run covers them all: there is nothing to
// redo.
func unheard(runs []soundRun, at []int64) (done int, holes [][2]int) {
	heard := func(r soundRun) bool {
		i, _ := slices.BinarySearch(at, r.startMs()-coverSlackMs)
		return i < len(at) && at[i] <= r.endMs()+coverSlackMs
	}
	for i := range runs {
		if heard(runs[i]) {
			done = i + 1
		}
	}
	if done == 0 {
		return len(runs), nil
	}
	for i := 0; i < done; {
		if heard(runs[i]) {
			i++
			continue
		}
		j, sound := i, int64(0)
		for ; !heard(runs[j]); j++ {
			sound += runs[j].endMs() - runs[j].startMs()
		}
		if sound >= minHoleMs {
			holes = append(holes, [2]int{i, j})
		}
		i = j
	}
	return done, holes
}
